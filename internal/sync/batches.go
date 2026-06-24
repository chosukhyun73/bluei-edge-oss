package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

// BatchEnvelope is the JSON payload POSTed to backend
// POST /v1/gx10/sync/batches. Field names match backend's GX10SyncBatchRequest
// Pydantic schema (docs/wip/backend-sync-endpoint-spec.md). EventCount reflects
// the number of events actually included in Events (after backend-supported
// event_type filtering), not the total number of events in the local batch.
type BatchEnvelope struct {
	BatchID       string           `json:"batch_id"`
	SchemaVersion string           `json:"schema_version"`
	SiteID        string           `json:"site_id"`
	EdgeID        string           `json:"edge_id"`
	FromSequence  int64            `json:"from_sequence"`
	ToSequence    int64            `json:"to_sequence"`
	EventCount    int              `json:"event_count"`
	GeneratedAt   string           `json:"generated_at"`
	Events        []map[string]any `json:"events"`
}

// edgeToBackendEventType maps edge-side event_type strings to backend's
// GX10SyncEvent.event_type Pydantic Literal. Returns ("", false) if the edge
// event type is not currently supported by backend ingest.
//
// Backend Literal accepts:
//
//	environment_snapshot, tank_state, feeding_event, device_health,
//	sensor.reading.recorded, device.health.updated
//
// Unsupported edge events are dropped from the envelope but still marked
// synced via batch ACK so they don't accumulate in the local queue.
// Phase 2: backend Literal expansion is needed for mortality / stocking /
// harvest / treatment / transfer / sampling / fcr / document / inventory /
// ai_decision / autonomous_action / alerts.
var edgeToBackendEventType = map[string]string{
	"sensor.reading.daily_summary": "environment_snapshot",
	"device.health.updated":        "device.health.updated",
	"feeding.recorded":             "feeding_event",
}

func mapEdgeEventTypeToBackend(edgeType string) (string, bool) {
	v, ok := edgeToBackendEventType[edgeType]
	return v, ok
}

// transformEventForBackend converts an edge storage.Event into a
// GX10SyncEvent-shaped map ready for JSON serialization. Returns (nil, false)
// if the edge event_type is not currently supported by backend ingest.
func transformEventForBackend(e *storage.Event) (map[string]any, bool) {
	backendType, supported := mapEdgeEventTypeToBackend(e.EventType)
	if !supported {
		return nil, false
	}

	var payload map[string]any
	if e.PayloadJSON != "" {
		_ = json.Unmarshal([]byte(e.PayloadJSON), &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}

	var gx10TankID string
	if v, ok := payload["tank_id"].(string); ok {
		gx10TankID = v
	}

	out := map[string]any{
		"event_id":    e.EventID,
		"event_type":  backendType,
		"recorded_at": e.RecordedAt.UTC().Format(time.RFC3339Nano),
		"payload":     payload,
	}
	if gx10TankID != "" {
		out["gx10_tank_id"] = gx10TankID
	}
	if e.SourceModule != "" || e.SourceAdapter != "" || e.SourceDevice != "" {
		out["source"] = map[string]string{
			"module":    e.SourceModule,
			"adapter":   e.SourceAdapter,
			"device_id": e.SourceDevice,
		}
	}
	return out, true
}

// createBatch assembles unsynced, unbatched events into a new sync batch.
func createBatch(ctx context.Context, app appInterface, store storage.Store, batchSize int) error {
	unsynced, err := store.ListUnsyncedUnbatchedEvents(ctx, batchSize)
	if err != nil {
		return err
	}
	if len(unsynced) == 0 {
		return nil
	}

	var minSeq, maxSeq int64
	seqs := make([]int64, 0, len(unsynced))
	for i, e := range unsynced {
		seqs = append(seqs, e.Sequence)
		if i == 0 {
			minSeq = e.Sequence
			maxSeq = e.Sequence
		} else {
			if e.Sequence < minSeq {
				minSeq = e.Sequence
			}
			if e.Sequence > maxSeq {
				maxSeq = e.Sequence
			}
		}
	}

	batchID := common.NewBatchID()
	b := &storage.SyncBatch{
		BatchID:      batchID,
		FromSequence: minSeq,
		ToSequence:   maxSeq,
		Status:       "created",
		CreatedAt:    common.NowUTC(),
	}

	if err := store.InsertSyncBatch(ctx, b, seqs); err != nil {
		return err
	}

	payload := map[string]any{
		"batch_id":      batchID,
		"from_sequence": minSeq,
		"to_sequence":   maxSeq,
		"event_count":   len(unsynced),
		"created_at":    common.FormatTime(b.CreatedAt),
	}
	if _, err := app.AppendEvent(ctx, "sync", "", "", "sync.batch.created", batchID, payload); err != nil {
		slog.Warn("failed to record batch.created event", "error", err)
	}

	slog.Info("sync batch created", "batch_id", batchID, "events", len(unsynced))
	return nil
}

type appInterface interface {
	AppendEvent(ctx context.Context, module, adapter, deviceID, eventType, corrID string, payload any) (int64, error)
}

// BuildBatchEnvelope materializes a persisted sync batch into the JSON-ready
// envelope used by the transmission layer. Each event is transformed into the
// backend GX10SyncEvent shape; edge event_types not in edgeToBackendEventType
// are dropped from envelope.Events (logged at Debug) but the batch as a whole
// is still ACKed on successful POST so unsupported events don't accumulate.
func BuildBatchEnvelope(ctx context.Context, store storage.Store, batch *storage.SyncBatch) (*BatchEnvelope, error) {
	if batch == nil {
		return nil, fmt.Errorf("batch is nil")
	}
	events, err := store.GetSyncBatchEvents(ctx, batch.BatchID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("batch %s has no events", batch.BatchID)
	}

	envelopeEvents := make([]map[string]any, 0, len(events))
	dropped := 0
	for _, e := range events {
		backendEvent, ok := transformEventForBackend(e)
		if !ok {
			dropped++
			continue
		}
		envelopeEvents = append(envelopeEvents, backendEvent)
	}
	if dropped > 0 {
		slog.Debug("sync envelope dropped unsupported events",
			"batch_id", batch.BatchID,
			"dropped", dropped,
			"included", len(envelopeEvents))
	}

	first := events[0]
	return &BatchEnvelope{
		BatchID:       batch.BatchID,
		SchemaVersion: "1.0",
		SiteID:        first.SiteID,
		EdgeID:        first.EdgeID,
		FromSequence:  batch.FromSequence,
		ToSequence:    batch.ToSequence,
		EventCount:    len(envelopeEvents),
		GeneratedAt:   common.FormatTime(common.NowUTC()),
		Events:        envelopeEvents,
	}, nil
}

// markBatchFailed records a batch failure.
func markBatchFailed(ctx context.Context, app appInterface, store storage.Store, batchID, errCode, errMsg string) {
	errObj := map[string]string{"code": errCode, "message": errMsg}
	errJSONBytes, _ := json.Marshal(errObj)
	if err := store.UpdateSyncBatchStatus(ctx, batchID, "failed", nil, nil, "", string(errJSONBytes)); err != nil {
		slog.Warn("failed to mark batch failed", "batch_id", batchID, "error", err)
	}
	payload := map[string]any{
		"batch_id":   batchID,
		"result":     "failed",
		"error_code": errCode,
		"message":    errMsg,
	}
	if _, err := app.AppendEvent(ctx, "sync", "", "", "sync.batch.failed", batchID, payload); err != nil {
		slog.Warn("failed to record batch.failed event", "error", err)
	}
}

// markBatchAcknowledged records a batch ack.
func markBatchAcknowledged(ctx context.Context, app appInterface, store storage.Store, batchID, remoteAckID string) {
	now := time.Now().UTC()
	if err := store.UpdateSyncBatchStatus(ctx, batchID, "acknowledged", nil, &now, remoteAckID, ""); err != nil {
		slog.Warn("failed to mark batch acknowledged", "batch_id", batchID, "error", err)
		return
	}
	payload := map[string]any{
		"batch_id":        batchID,
		"remote_ack_id":   remoteAckID,
		"acknowledged_at": common.FormatTime(now),
	}
	if _, err := app.AppendEvent(ctx, "sync", "", "", "sync.batch.acknowledged", batchID, payload); err != nil {
		slog.Warn("failed to record batch.acknowledged event", "error", err)
	}
}
