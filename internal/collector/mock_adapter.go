package collector

import (
	"context"
	"log/slog"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/runtime"
)

// MockAdapter emits synthetic sensor readings on a fixed interval.
type MockAdapter struct {
	app       *runtime.App
	cfg       config.CollectorAdapter
	devices   *config.DevicesConfig
	deviceMap map[string]config.DeviceEntry
}

func NewMockAdapter(app *runtime.App, cfg config.CollectorAdapter, devices *config.DevicesConfig) *MockAdapter {
	m := make(map[string]config.DeviceEntry)
	if devices != nil {
		for _, d := range devices.Devices {
			m[d.DeviceID] = d
		}
	}
	return &MockAdapter{app: app, cfg: cfg, devices: devices, deviceMap: m}
}

func (a *MockAdapter) ID() string { return a.cfg.ID }

func (a *MockAdapter) Type() string { return a.cfg.Type }

func (a *MockAdapter) Run(ctx context.Context) {
	interval := time.Duration(a.cfg.IntervalSec) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Emit once at startup so local state is visible immediately after the
	// collector starts; the ticker maintains the configured cadence afterward.
	a.emit(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.emit(ctx)
		}
	}
}

func (a *MockAdapter) emit(ctx context.Context) {
	now := common.NowUTC()
	seenDevices := make(map[string]struct{})

	for _, m := range a.cfg.Metrics {
		readingID := common.NewReadingID()
		value := m.Value
		loc := a.locationForDevice(m.DeviceID)
		payload := events.SensorReadingPayload{
			ReadingID:  readingID,
			SensorID:   m.SensorID,
			DeviceID:   m.DeviceID,
			Metric:     m.Metric,
			Value:      &value,
			Unit:       m.Unit,
			Quality:    events.QualityOK,
			ObservedAt: common.FormatTime(now),
			Location:   loc,
			Raw: map[string]any{
				"mock":       true,
				"adapter_id": a.cfg.ID,
			},
		}
		if err := payload.Validate(); err != nil {
			slog.Error("mock sensor payload validation failed", "adapter", a.cfg.ID, "sensor_id", m.SensorID, "error", err)
			a.recordCollectorError(ctx, m.DeviceID, "VALIDATION_FAILED", err.Error())
			continue
		}

		seq, err := a.app.AppendEvent(ctx, "collector", a.cfg.ID, m.DeviceID,
			events.EventSensorReadingRecorded, readingID, payload)
		if err != nil {
			slog.Error("collector append event failed", "adapter", a.cfg.ID, "error", err)
			a.recordCollectorError(ctx, m.DeviceID, "APPEND_FAILED", err.Error())
			continue
		}
		seenDevices[m.DeviceID] = struct{}{}
		a.app.Health.Touch("collector")
		slog.Debug("sensor reading recorded", "adapter", a.cfg.ID, "sensor_id", m.SensorID, "seq", seq)
	}

	for deviceID := range seenDevices {
		a.emitDeviceHealth(ctx, deviceID, now)
	}
}

func (a *MockAdapter) emitDeviceHealth(ctx context.Context, deviceID string, observedAt time.Time) {
	device, ok := a.deviceMap[deviceID]
	deviceType := events.DeviceTypeUnknown
	tankID := ""
	if ok {
		deviceType = device.DeviceType
		tankID = device.Location.TankID
	}
	payload := events.DeviceHealthPayload{
		DeviceID:   deviceID,
		DeviceType: deviceType,
		TankID:     tankID,
		Status:     events.DeviceStatusOnline,
		Quality:    events.QualityOK,
		LastSeenAt: common.FormatTime(observedAt),
		ErrorCode:  nil,
		Details: map[string]any{
			"mock":       true,
			"adapter_id": a.cfg.ID,
		},
	}
	if err := payload.Validate(); err != nil {
		slog.Error("mock device health payload validation failed", "adapter", a.cfg.ID, "device_id", deviceID, "error", err)
		a.recordCollectorError(ctx, deviceID, "DEVICE_HEALTH_VALIDATION_FAILED", err.Error())
		return
	}
	if _, err := a.app.AppendEvent(ctx, "collector", a.cfg.ID, deviceID,
		events.EventDeviceHealthUpdated, deviceID, payload); err != nil {
		slog.Error("collector append device health failed", "adapter", a.cfg.ID, "device_id", deviceID, "error", err)
		a.recordCollectorError(ctx, deviceID, "DEVICE_HEALTH_APPEND_FAILED", err.Error())
	}
}

func (a *MockAdapter) locationForDevice(deviceID string) events.Location {
	device, ok := a.deviceMap[deviceID]
	if !ok {
		return events.Location{}
	}
	return events.Location{
		AreaID:         device.Location.AreaID,
		TankID:         device.Location.TankID,
		PlatformTankID: device.Location.PlatformTankID,
	}
}

func (a *MockAdapter) recordCollectorError(ctx context.Context, deviceID, code, msg string) {
	payload := map[string]any{
		"adapter_id":  a.cfg.ID,
		"device_id":   deviceID,
		"error_code":  code,
		"message":     msg,
		"retryable":   true,
		"occurred_at": common.FormatTime(common.NowUTC()),
	}
	if _, err := a.app.AppendEvent(ctx, "collector", a.cfg.ID, deviceID,
		"collector.error.recorded", "", payload); err != nil {
		slog.Warn("failed to record collector error event", "error", err)
	}
}
