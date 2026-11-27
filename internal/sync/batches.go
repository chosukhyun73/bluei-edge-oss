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
// 생산 기록(stocking/harvest/transfer/treatment/mortality/sampling)은 2단계에서 추가.
// 남은 것: fcr / document / inventory / ai_decision / autonomous_action / alerts.
var edgeToBackendEventType = map[string]string{
	// 실시간 수질(수온/DO/pH/…) — 백엔드가 env_readings(long-format)로 투영한다.
	// 2026-07-17 종단 검증(synccheck): 실 water_temperature POST → projected=1 확인.
	"sensor.reading.recorded":      "sensor.reading.recorded",
	"sensor.reading.daily_summary": "environment_snapshot",
	"device.health.updated":        "device.health.updated",
	// 취수구(연안) 수온 — NIFS 강릉 관측소 해수온. GX10 로컬에만 있던 external.env.recorded
	// (payload {metric:sea_water_temperature, value, unit})를 백엔드 env_readings 로 흘려보내
	// 앱이 "수조 수온"과 "취수구 수온"을 함께 표시할 수 있게 한다. tank_id 없음 → 백엔드
	// group_fallback 으로 노드 대표 수조에 sea_water_temperature 지표로 투영.
	"external.env.recorded": "sensor.reading.recorded",
	// feeding_event 는 payload 필드 매핑(actual_total_amount_g→feed_amount_g 등)이 필요해
	// feed.cycle.report 는 아직 미포함(Phase 1b). "feeding.recorded" 는 현재 미발생(레거시).
	"feeding.recorded": "feeding_event",
	// 생산 기록(docs/53 2단계) — 서버가 앱과 같은 helper 로 입식·분조·출하 lot·QR 까지 만든다.
	// 마릿수는 /gx10/tanks 가 정본이라 서버는 마릿수를 바꾸지 않는다.
	"tank.stocking.recorded":  "tank.stocking",
	"tank.harvest.recorded":   "tank.harvest",
	"tank.transfer.recorded":  "tank.transfer",
	"tank.treatment.recorded": "tank.treatment",
	"tank.mortality.recorded": "tank.mortality",
	"tank.sampling.recorded":  "tank.sampling",
}

func mapEdgeEventTypeToBackend(edgeType string) (string, bool) {
	v, ok := edgeToBackendEventType[edgeType]
	return v, ok
}

// supportedEdgeEventTypes returns the edge event_types the backend can ingest.
// createBatch queues only these so batches stay dense with projectable events;
// unsupported high-volume types (media.clip/snapshot/feed.*) never enter the
// sync queue and are handled by retention (or a separate pull path for clips).
func supportedEdgeEventTypes() []string {
	out := make([]string, 0, len(edgeToBackendEventType))
	for k := range edgeToBackendEventType {
		out = append(out, k)
	}
	return out
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

	// 앱에서 이어받은 입식은 앱에 이미 lot 이 있다 — 다시 올리지 않는다(배치 ACK 로 synced 처리).
	if e.EventType == "tank.stocking.recorded" && payload["imported_from"] == "platform" {
		return nil, false
	}

	var gx10TankID string
	if v, ok := payload["tank_id"].(string); ok {
		gx10TankID = v
	}
	// 분조·이동·판매는 tank_id 대신 출발 수조(from_tank_id)를 가진다.
	if v, ok := payload["from_tank_id"].(string); ok && gx10TankID == "" {
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
	unsynced, err := store.ListUnsyncedUnbatchedEvents(ctx, supportedEdgeEventTypes(), batchSize)
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
	// ACK 된 이벤트를 synced 로 표시한다. 빠지면 같은 이벤트가 매 배치에 다시 뽑혀
	// 새 이벤트가 전송되지 않는다(2026-07-19~ 선두 차단). 실패해도 재전송은 백엔드
	// event_id 중복 무시로 무해하므로 경고만 남긴다.
	if events, err := store.GetSyncBatchEvents(ctx, batchID); err != nil {
		slog.Warn("failed to load acknowledged batch events", "batch_id", batchID, "error", err)
	} else {
		ids := make([]string, 0, len(events))
		for _, e := range events {
			ids = append(ids, e.EventID)
		}
		if err := store.MarkSynced(ctx, ids); err != nil {
			slog.Warn("failed to mark acknowledged events synced", "batch_id", batchID, "error", err)
		}
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
