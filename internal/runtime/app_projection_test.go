package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

func openProjectionTestApp(t *testing.T) (*App, storage.Store) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "projection.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := storage.Migrate(store, filepath.Join("..", "..", "migrations", "001_init.sql")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	app := NewApp(&config.Config{
		Site: config.SiteConfig{SiteID: "site_test"},
		Edge: config.EdgeConfig{EdgeID: "edge_test"},
	}, store)
	return app, store
}

func TestAppendEventProjectsSensorReadingToTankEnvironment(t *testing.T) {
	ctx := context.Background()
	app, store := openProjectionTestApp(t)
	value := 18.7
	payload := events.SensorReadingPayload{
		ReadingID:  "reading_001",
		SensorID:   "sensor_temp_01",
		DeviceID:   "mock_probe_01",
		Metric:     events.MetricWaterTemperature,
		Value:      &value,
		Unit:       "celsius",
		Quality:    events.QualityOK,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Location: events.Location{
			TankID:         "tank_01",
			PlatformTankID: "00000000-0000-0000-0000-000000000001",
		},
	}
	if _, err := app.AppendEvent(ctx, "collector", "mock-water-01", "mock_probe_01", events.EventSensorReadingRecorded, "", payload); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	readings, err := store.ListTankEnvironment(ctx, "tank_01")
	if err != nil {
		t.Fatalf("ListTankEnvironment: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("expected 1 projected environment reading, got %d", len(readings))
	}
	if readings[0].Metric != events.MetricWaterTemperature || readings[0].Value == nil || *readings[0].Value != value {
		t.Fatalf("unexpected projected environment reading: %#v", readings[0])
	}
}

func TestAppendEventProjectsDeviceHealthStatus(t *testing.T) {
	ctx := context.Background()
	app, store := openProjectionTestApp(t)

	lastSeenAt := time.Now().UTC()
	payload := events.DeviceHealthPayload{
		DeviceID:   "mock_probe_01",
		DeviceType: events.DeviceTypeWaterQualitySensor,
		TankID:     "tank_01",
		Status:     events.DeviceStatusOnline,
		Quality:    events.QualityOK,
		LastSeenAt: lastSeenAt.Format(time.RFC3339Nano),
		Details: map[string]any{
			"mock": true,
		},
	}
	if _, err := app.AppendEvent(ctx, "collector", "mock-water-01", "mock_probe_01", events.EventDeviceHealthUpdated, "", payload); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	devices, err := store.ListDeviceStatuses(ctx)
	if err != nil {
		t.Fatalf("ListDeviceStatuses: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 projected device, got %d", len(devices))
	}
	device := devices[0]
	if device["device_id"] != "mock_probe_01" || device["status"] != events.DeviceStatusOnline || device["health"] != events.QualityOK {
		t.Fatalf("unexpected projected device: %#v", device)
	}
	if device["last_event_id"] == "" {
		t.Fatalf("expected projected last_event_id: %#v", device)
	}
}

func TestAppendEventProjectsCameraHealthStatus(t *testing.T) {
	ctx := context.Background()
	app, store := openProjectionTestApp(t)

	lastFrameAt := time.Now().UTC()
	payload := events.CameraHealthPayload{
		CameraID:       "cam_tank_01_front",
		TankID:         "tank_01",
		Status:         events.DeviceStatusOnline,
		IngestFPS:      25.0,
		LastFrameAt:    lastFrameAt.Format(time.RFC3339Nano),
		ReconnectCount: 0,
		DroppedFrames:  2,
	}
	if _, err := app.AppendEvent(ctx, "vision", "hikvision", "cam_tank_01_front", events.EventCameraHealthUpdated, "", payload); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	cameras, err := store.ListCameraStatuses(ctx, "tank_01")
	if err != nil {
		t.Fatalf("ListCameraStatuses: %v", err)
	}
	if len(cameras) != 1 {
		t.Fatalf("expected 1 projected camera, got %d", len(cameras))
	}
	camera := cameras[0]
	if camera.CameraID != "cam_tank_01_front" || camera.Status != events.DeviceStatusOnline || camera.IngestFPS != 25.0 {
		t.Fatalf("unexpected projected camera: %#v", camera)
	}
	if camera.LastEventID == "" {
		t.Fatalf("expected projected last_event_id: %#v", camera)
	}
}
