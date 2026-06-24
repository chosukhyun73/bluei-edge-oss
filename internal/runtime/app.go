package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

const Version = "0.1.0"

// App holds the wired-up application state.
type App struct {
	Config  *config.Config
	Store   storage.Store
	Health  *HealthRegistry
	Manager *Manager

	siteID    string
	edgeID    string
	startedAt time.Time
}

// NewApp creates an App without starting any services.
func NewApp(cfg *config.Config, store storage.Store) *App {
	return &App{
		Config:    cfg,
		Store:     store,
		Health:    NewHealthRegistry(),
		siteID:    cfg.Site.SiteID,
		edgeID:    cfg.Edge.EdgeID,
		startedAt: common.NowUTC(),
	}
}

// RecordStartup writes the runtime.started event and checks for unclean shutdown.
func (a *App) RecordStartup(ctx context.Context, configHash string) error {
	// Detect unclean shutdown
	lastClean, ok, err := a.Store.KVGet(ctx, "last_clean_shutdown_at")
	if err != nil {
		return fmt.Errorf("kv get last_clean_shutdown_at: %w", err)
	}
	lastStartup, _, _ := a.Store.KVGet(ctx, "last_startup_at")
	if !ok && lastStartup != "" {
		// previous startup without clean shutdown
		payload := map[string]any{
			"last_startup_at":        lastStartup,
			"last_clean_shutdown_at": nil,
			"detected_at":            common.FormatTime(common.NowUTC()),
		}
		if err := a.appendRuntimeEvent(ctx, "runtime.previous_shutdown_unclean", payload, ""); err != nil {
			slog.Warn("failed to record unclean shutdown event", "error", err)
		}
	}
	_ = lastClean

	// Record startup
	payload := map[string]any{
		"version":      Version,
		"pid":          0, // not filling OS pid in skeleton
		"config_hash":  configHash,
		"offline_mode": a.Config.Sync.Endpoint == nil,
		"started_at":   common.FormatTime(a.startedAt),
	}
	if err := a.appendRuntimeEvent(ctx, "runtime.started", payload, ""); err != nil {
		return fmt.Errorf("record startup event: %w", err)
	}

	// Mark last startup
	return a.Store.KVSet(ctx, "last_startup_at", common.FormatTime(a.startedAt))
}

// RecordShutdown writes the runtime.stopped event and marks clean shutdown.
func (a *App) RecordShutdown(ctx context.Context, reason, signal string) {
	payload := map[string]any{
		"reason":     reason,
		"signal":     signal,
		"stopped_at": common.FormatTime(common.NowUTC()),
	}
	if err := a.appendRuntimeEvent(ctx, "runtime.stopped", payload, ""); err != nil {
		slog.Warn("failed to record shutdown event", "error", err)
	}
	if err := a.Store.KVSet(ctx, "last_clean_shutdown_at", common.FormatTime(common.NowUTC())); err != nil {
		slog.Warn("failed to mark clean shutdown", "error", err)
	}
}

func (a *App) appendRuntimeEvent(ctx context.Context, eventType string, payload any, corrID string) error {
	payloadJSON, _ := json.Marshal(payload)
	now := common.NowUTC()
	eventID := common.NewEventID()
	envelope := map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": "1.0",
		"site_id":        a.siteID,
		"edge_id":        a.edgeID,
		"source":         map[string]any{"module": "runtime", "adapter": nil, "device_id": nil},
		"recorded_at":    common.FormatTime(now),
		"payload":        payload,
	}
	if corrID != "" {
		envelope["trace"] = map[string]any{"correlation_id": corrID, "causation_id": nil}
	}
	envelopeJSON, _ := json.Marshal(envelope)

	_, err := a.Store.AppendEvent(ctx, &storage.Event{
		EventID:       eventID,
		EventType:     eventType,
		SchemaVersion: "1.0",
		SiteID:        a.siteID,
		EdgeID:        a.edgeID,
		RecordedAt:    now,
		SourceModule:  "runtime",
		PayloadJSON:   string(payloadJSON),
		EventJSON:     string(envelopeJSON),
		CorrelationID: corrID,
	})
	return err
}

// AppendEvent is a helper used by sub-packages to write events with the standard envelope.
func (a *App) AppendEvent(ctx context.Context, module, adapter, deviceID, eventType, corrID string, payload any) (int64, error) {
	return a.appendEvent(ctx, common.NewEventID(), module, adapter, deviceID, eventType, corrID, payload)
}

// AppendEventWithID writes an event with a caller-provided deterministic event_id.
// Use only for replay/import paths that need idempotent event identity.
func (a *App) AppendEventWithID(ctx context.Context, eventID, module, adapter, deviceID, eventType, corrID string, payload any) (int64, error) {
	return a.appendEvent(ctx, eventID, module, adapter, deviceID, eventType, corrID, payload)
}

func (a *App) appendEvent(ctx context.Context, eventID, module, adapter, deviceID, eventType, corrID string, payload any) (int64, error) {
	payloadJSON, _ := json.Marshal(payload)
	now := common.NowUTC()
	envelope := map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": "1.0",
		"site_id":        a.siteID,
		"edge_id":        a.edgeID,
		"source":         map[string]any{"module": module, "adapter": adapter, "device_id": deviceID},
		"recorded_at":    common.FormatTime(now),
		"payload":        payload,
	}
	if corrID != "" {
		envelope["trace"] = map[string]any{"correlation_id": corrID, "causation_id": nil}
	}
	envelopeJSON, _ := json.Marshal(envelope)

	seq, err := a.Store.AppendEvent(ctx, &storage.Event{
		EventID:       eventID,
		EventType:     eventType,
		SchemaVersion: "1.0",
		SiteID:        a.siteID,
		EdgeID:        a.edgeID,
		RecordedAt:    now,
		SourceModule:  module,
		SourceAdapter: adapter,
		SourceDevice:  deviceID,
		PayloadJSON:   string(payloadJSON),
		EventJSON:     string(envelopeJSON),
		CorrelationID: corrID,
	})
	if err != nil {
		return 0, err
	}
	if err := a.projectCurrentState(ctx, eventID, eventType, payloadJSON); err != nil {
		slog.Warn("failed to project current state", "event_id", eventID, "event_type", eventType, "error", err)
	}
	return seq, nil
}

func (a *App) projectCurrentState(ctx context.Context, eventID, eventType string, payloadJSON []byte) error {
	switch eventType {
	case events.EventSensorReadingRecorded:
		var payload events.SensorReadingPayload
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			return fmt.Errorf("decode sensor reading payload: %w", err)
		}
		if err := payload.Validate(); err != nil {
			return fmt.Errorf("validate sensor reading payload: %w", err)
		}
		if payload.Location.TankID == "" {
			return nil
		}
		return a.Store.UpsertTankEnvironmentReading(ctx, &storage.CurrentTankEnvironmentReading{
			TankID:      payload.Location.TankID,
			Metric:      payload.Metric,
			Value:       payload.Value,
			Unit:        payload.Unit,
			Quality:     payload.Quality,
			SensorID:    payload.SensorID,
			DeviceID:    payload.DeviceID,
			LastEventID: eventID,
			ObservedAt:  payload.ObservedAt,
		}, string(payloadJSON))
	case events.EventDeviceHealthUpdated:
		var payload events.DeviceHealthPayload
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			return fmt.Errorf("decode device health payload: %w", err)
		}
		if err := payload.Validate(); err != nil {
			return fmt.Errorf("validate device health payload: %w", err)
		}
		var lastSeenAt *time.Time
		if payload.LastSeenAt != "" {
			parsed, err := time.Parse(time.RFC3339Nano, payload.LastSeenAt)
			if err != nil {
				return fmt.Errorf("parse last_seen_at: %w", err)
			}
			lastSeenAt = &parsed
		}
		return a.Store.UpsertDeviceStatus(ctx, payload.DeviceID, payload.DeviceType, payload.Status, payload.Quality, eventID, lastSeenAt, string(payloadJSON))
	case events.EventCameraHealthUpdated:
		var payload events.CameraHealthPayload
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			return fmt.Errorf("decode camera health payload: %w", err)
		}
		if err := payload.Validate(); err != nil {
			return fmt.Errorf("validate camera health payload: %w", err)
		}
		return a.Store.UpsertCameraStatus(ctx, &storage.CurrentCameraStatus{
			CameraID:       payload.CameraID,
			TankID:         payload.TankID,
			Status:         payload.Status,
			IngestFPS:      payload.IngestFPS,
			LastEventID:    eventID,
			LastFrameAt:    payload.LastFrameAt,
			ReconnectCount: payload.ReconnectCount,
			DroppedFrames:  payload.DroppedFrames,
		}, string(payloadJSON))
	default:
		return nil
	}
}

func (a *App) SiteID() string       { return a.siteID }
func (a *App) EdgeID() string       { return a.edgeID }
func (a *App) StartedAt() time.Time { return a.startedAt }
