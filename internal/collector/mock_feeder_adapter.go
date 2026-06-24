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

// MockFeederAdapter emits feeder health and feed inventory readings.
// It is a structural placeholder for real feeder/vendor adapters.
type MockFeederAdapter struct {
	app       *runtime.App
	cfg       config.CollectorAdapter
	devices   *config.DevicesConfig
	deviceMap map[string]config.DeviceEntry
}

func NewMockFeederAdapter(app *runtime.App, cfg config.CollectorAdapter, devices *config.DevicesConfig) *MockFeederAdapter {
	m := make(map[string]config.DeviceEntry)
	if devices != nil {
		for _, d := range devices.Devices {
			m[d.DeviceID] = d
		}
	}
	return &MockFeederAdapter{app: app, cfg: cfg, devices: devices, deviceMap: m}
}

func (a *MockFeederAdapter) ID() string { return a.cfg.ID }

func (a *MockFeederAdapter) Type() string { return a.cfg.Type }

func (a *MockFeederAdapter) Run(ctx context.Context) {
	interval := time.Duration(a.cfg.IntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
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

func (a *MockFeederAdapter) emit(ctx context.Context) {
	now := common.NowUTC()
	seen := make(map[string]struct{})
	for _, m := range a.cfg.Metrics {
		value := m.Value
		loc := a.locationForDevice(m.DeviceID)
		payload := events.SensorReadingPayload{
			ReadingID:  common.NewReadingID(),
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
				"domain":     "feeder",
			},
		}
		if err := payload.Validate(); err != nil {
			slog.Error("mock feeder payload validation failed", "adapter", a.cfg.ID, "sensor_id", m.SensorID, "error", err)
			continue
		}
		if _, err := a.app.AppendEvent(ctx, "collector", a.cfg.ID, m.DeviceID, events.EventSensorReadingRecorded, payload.ReadingID, payload); err != nil {
			slog.Error("mock feeder reading append failed", "adapter", a.cfg.ID, "error", err)
			continue
		}
		seen[m.DeviceID] = struct{}{}
	}
	for deviceID := range seen {
		a.emitFeederHealth(ctx, deviceID, now)
	}
}

func (a *MockFeederAdapter) emitFeederHealth(ctx context.Context, deviceID string, observedAt time.Time) {
	device, ok := a.deviceMap[deviceID]
	deviceType := events.DeviceTypeFeeder
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
		Details: map[string]any{
			"mock":       true,
			"adapter_id": a.cfg.ID,
			"domain":     "feeder",
		},
	}
	if err := payload.Validate(); err != nil {
		slog.Error("mock feeder health validation failed", "adapter", a.cfg.ID, "device_id", deviceID, "error", err)
		return
	}
	if _, err := a.app.AppendEvent(ctx, "collector", a.cfg.ID, deviceID, events.EventDeviceHealthUpdated, deviceID, payload); err != nil {
		slog.Error("mock feeder health append failed", "adapter", a.cfg.ID, "device_id", deviceID, "error", err)
	}
}

func (a *MockFeederAdapter) locationForDevice(deviceID string) events.Location {
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
