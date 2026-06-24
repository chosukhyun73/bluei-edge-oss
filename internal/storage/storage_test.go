package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
)

const migSQL = `
PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL UNIQUE,
  event_type TEXT NOT NULL,
  schema_version TEXT NOT NULL,
  site_id TEXT NOT NULL,
  edge_id TEXT NOT NULL,
  recorded_at TEXT NOT NULL,
  source_module TEXT NOT NULL,
  source_adapter TEXT,
  source_device_id TEXT,
  payload_json TEXT NOT NULL,
  event_json TEXT NOT NULL,
  correlation_id TEXT,
  causation_id TEXT,
  synced_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE TABLE IF NOT EXISTS current_device_status (
  device_id TEXT PRIMARY KEY, device_type TEXT NOT NULL,
  status TEXT NOT NULL, health TEXT NOT NULL,
  last_event_id TEXT NOT NULL, last_seen_at TEXT,
  updated_at TEXT NOT NULL, details_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS current_tank_environment (
  tank_id TEXT NOT NULL, metric TEXT NOT NULL,
  value REAL, unit TEXT NOT NULL, quality TEXT NOT NULL,
  sensor_id TEXT NOT NULL, device_id TEXT NOT NULL,
  last_event_id TEXT NOT NULL, observed_at TEXT NOT NULL,
  updated_at TEXT NOT NULL, payload_json TEXT NOT NULL,
  PRIMARY KEY (tank_id, metric)
);
CREATE TABLE IF NOT EXISTS current_camera_status (
  camera_id TEXT PRIMARY KEY, tank_id TEXT, status TEXT NOT NULL,
  ingest_fps REAL NOT NULL, last_event_id TEXT NOT NULL, last_frame_at TEXT,
  reconnect_count INTEGER NOT NULL, dropped_frames INTEGER NOT NULL,
  updated_at TEXT NOT NULL, details_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS camera_profiles (
  camera_id TEXT PRIMARY KEY,
  tank_id TEXT,
  display_name TEXT NOT NULL,
  vendor TEXT,
  host TEXT,
  rtsp_port INTEGER,
  http_port INTEGER,
  username TEXT,
  password_secret_ref TEXT,
  position TEXT,
  purpose_json TEXT NOT NULL,
  stream_profiles_json TEXT NOT NULL,
  clip_policy_json TEXT NOT NULL,
  status TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  model_id TEXT,
  mounting_height_m REAL,
  underwater_depth_m REAL,
  mount_location TEXT,
  view_angle TEXT,
  height_from_water_m REAL,
  tilt_deg REAL
);
CREATE TABLE IF NOT EXISTS camera_models (
  model_id TEXT PRIMARY KEY,
  vendor TEXT NOT NULL,
  product_code TEXT NOT NULL,
  display_name TEXT NOT NULL,
  lens_type TEXT NOT NULL CHECK (lens_type IN ('single','dual','fisheye','ptz','other')),
  baseline_mm REAL,
  stereo_calibration_json TEXT,
  resolution_w INTEGER,
  resolution_h INTEGER,
  fov_deg REAL,
  fps INTEGER,
  night_mode INTEGER NOT NULL DEFAULT 0,
  protocols_json TEXT,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tank_profiles (
  tank_id TEXT PRIMARY KEY,
  platform_tank_id TEXT,
  display_name TEXT NOT NULL,
  species TEXT NOT NULL,
  system_type TEXT NOT NULL,
  volume_m3 REAL,
  biomass_kg REAL,
  fish_count INTEGER,
  avg_weight_g REAL,
  target_ranges_json TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  group_id TEXT,
  site_id TEXT,
  wtg_id TEXT,
  lot_no TEXT,
  lifecycle_stage TEXT,
  mutable_lifecycle INTEGER NOT NULL DEFAULT 0,
  form_factor TEXT,
  diameter_m REAL,
  length_m REAL,
  width_m REAL,
  depth_m REAL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tank_profiles_group ON tank_profiles(group_id);
CREATE INDEX IF NOT EXISTS idx_tank_profiles_site  ON tank_profiles(site_id);
CREATE INDEX IF NOT EXISTS idx_tank_profiles_wtg   ON tank_profiles(wtg_id);
CREATE INDEX IF NOT EXISTS idx_tank_profiles_lot   ON tank_profiles(lot_no);
CREATE TABLE IF NOT EXISTS group_profiles (
  group_id      TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  description   TEXT NOT NULL DEFAULT '',
  color         TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS open_alerts (
  alert_id TEXT PRIMARY KEY, alert_dedupe_key TEXT NOT NULL UNIQUE,
  alert_type TEXT NOT NULL, severity TEXT NOT NULL,
  subject_kind TEXT NOT NULL, subject_id TEXT NOT NULL,
  rule_id TEXT, status TEXT NOT NULL,
  raised_at TEXT NOT NULL, updated_at TEXT NOT NULL, payload_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS control_commands (
  command_id TEXT PRIMARY KEY, idempotency_key TEXT NOT NULL UNIQUE,
  target_device_id TEXT NOT NULL, command_type TEXT NOT NULL,
  status TEXT NOT NULL, requested_at TEXT NOT NULL,
  expires_at TEXT NOT NULL, last_event_id TEXT NOT NULL, payload_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sync_batches (
  batch_id TEXT PRIMARY KEY,
  from_sequence INTEGER NOT NULL, to_sequence INTEGER NOT NULL,
  status TEXT NOT NULL, attempt INTEGER NOT NULL DEFAULT 0,
  next_retry_at TEXT, created_at TEXT NOT NULL,
  sent_at TEXT, acknowledged_at TEXT, remote_ack_id TEXT, error_json TEXT
);
CREATE TABLE IF NOT EXISTS sync_batch_events (
  batch_id TEXT NOT NULL, event_sequence INTEGER NOT NULL,
  event_id TEXT NOT NULL, status TEXT NOT NULL,
  error_code TEXT, error_message TEXT,
  PRIMARY KEY (batch_id, event_sequence)
);
CREATE TABLE IF NOT EXISTS runtime_kv (
  key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS water_quality_buckets (
  tank_id TEXT NOT NULL,
  bucket_start TEXT NOT NULL,
  bucket_sec INTEGER NOT NULL,
  temperature_c_avg REAL,
  ph_avg REAL,
  do_mg_l_avg REAL,
  quality TEXT NOT NULL,
  sample_count INTEGER NOT NULL,
  suspect_count INTEGER NOT NULL,
  source_reading_ids_json TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (tank_id, bucket_start)
);
CREATE TABLE IF NOT EXISTS feeding_impact_analyses (
  feeding_id TEXT PRIMARY KEY,
  analysis_id TEXT NOT NULL UNIQUE,
  tank_id TEXT NOT NULL,
  feed_amount_g REAL NOT NULL,
  fed_at TEXT NOT NULL,
  do_baseline_mg_l REAL,
  do_min_post_mg_l REAL,
  do_drop_mg_l REAL,
  do_recovery_min INTEGER,
  ph_delta REAL,
  temp_delta_c REAL,
  quality TEXT NOT NULL,
  reason_codes_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS current_tank_autonomous_mode (
  tank_id    TEXT PRIMARY KEY,
  mode       TEXT NOT NULL,
  reason     TEXT,
  changed_at TEXT NOT NULL,
  changed_by TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS current_tank_lifecycle (
  tank_id                 TEXT PRIMARY KEY,
  active_stocking_id      TEXT NOT NULL,
  species                 TEXT NOT NULL,
  growth_stage            TEXT NOT NULL,
  initial_count           INTEGER NOT NULL,
  initial_avg_weight_g    REAL NOT NULL,
  target_harvest_weight_g REAL,
  target_harvest_date     TEXT,
  source_hatchery         TEXT,
  stocked_at              TEXT NOT NULL,
  status                  TEXT NOT NULL,
  updated_at              TEXT NOT NULL,
  lot_no                  TEXT,
  parent_lot_no           TEXT
);
CREATE TABLE IF NOT EXISTS current_tank_sampling (
  tank_id              TEXT PRIMARY KEY,
  latest_sampling_id   TEXT NOT NULL,
  stocking_id          TEXT,
  sampled_count        INTEGER NOT NULL,
  avg_weight_g         REAL NOT NULL,
  std_weight_g         REAL,
  min_weight_g         REAL,
  max_weight_g         REAL,
  health_score         INTEGER,
  health_notes         TEXT,
  abnormal_count       INTEGER,
  sampled_at           TEXT NOT NULL,
  recorded_by          TEXT NOT NULL,
  updated_at           TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS current_tank_fcr_calibration (
  tank_id            TEXT PRIMARY KEY,
  stocking_id        TEXT NOT NULL,
  sampling_id        TEXT NOT NULL,
  default_fcr        REAL NOT NULL,
  observed_fcr       REAL NOT NULL,
  calibrated_fcr     REAL NOT NULL,
  deviation_pct      REAL NOT NULL,
  cumulative_feed_g  REAL NOT NULL,
  delta_biomass_g    REAL NOT NULL,
  calibrated_at      TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS current_tank_decision_policy (
  tank_id              TEXT PRIMARY KEY,
  auto_execute_enabled INTEGER NOT NULL DEFAULT 0,
  grace_minutes        INTEGER NOT NULL DEFAULT 10,
  updated_at           TEXT NOT NULL,
  updated_by           TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tank_weight_history (
  tank_id                TEXT NOT NULL,
  snapshot_date          TEXT NOT NULL,
  estimated_avg_weight_g REAL NOT NULL,
  anchor_weight_g        REAL NOT NULL,
  anchor_source          TEXT NOT NULL,
  days_since_anchor      INTEGER NOT NULL,
  expected_fcr           REAL NOT NULL,
  fcr_source             TEXT NOT NULL,
  cumulative_feed_g      REAL NOT NULL,
  quality                TEXT NOT NULL,
  snapshot_at            TEXT NOT NULL,
  PRIMARY KEY (tank_id, snapshot_date)
);
CREATE INDEX IF NOT EXISTS idx_weight_history_tank_date
  ON tank_weight_history (tank_id, snapshot_date DESC);
CREATE TABLE IF NOT EXISTS actuator_models (
  model_id TEXT PRIMARY KEY,
  vendor TEXT NOT NULL,
  product_code TEXT NOT NULL,
  display_name TEXT NOT NULL,
  device_category TEXT NOT NULL CHECK (device_category IN (
    'pump','aerator','oxygen_cone','heater','chiller','uv_sterilizer',
    'led_light','feeder','valve','biofilter','drum_filter','dosing_pump',
    'ozonator','blower','skimmer','other',
    'circulation_pump','heat_pump','air_pump'
  )),
  rated_power_w REAL,
  capacity_value REAL,
  capacity_unit TEXT,
  control_method TEXT CHECK (control_method IN (
    'on_off','pwm','4-20ma','0-10v','modbus','mqtt','esp32_controller','manual','other'
  )),
  response_time_s REAL,
  control_range_min REAL,
  control_range_max REAL,
  control_range_unit TEXT,
  consumable_replacement_days INTEGER,
  notes TEXT,
  category_specs TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`

func openTestStore(t *testing.T) storage.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Write migration SQL to a temp file and migrate
	sqlFile := filepath.Join(dir, "mig.sql")
	if err := os.WriteFile(sqlFile, []byte(migSQL), 0o600); err != nil {
		t.Fatalf("write sql: %v", err)
	}
	if err := storage.Migrate(store, sqlFile); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestAppendAndQueryEvents(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	evt := &storage.Event{
		EventID:       "evt_test_001",
		EventType:     "sensor.reading.recorded",
		SchemaVersion: "1.0",
		SiteID:        "site_test",
		EdgeID:        "edge_test",
		RecordedAt:    time.Now().UTC(),
		SourceModule:  "collector",
		PayloadJSON:   `{"metric":"dissolved_oxygen","value":6.7}`,
		EventJSON:     `{"event_id":"evt_test_001"}`,
	}

	seq, err := store.AppendEvent(ctx, evt)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if seq < 1 {
		t.Errorf("expected seq >= 1, got %d", seq)
	}

	events, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: "sensor.reading.recorded",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventID != "evt_test_001" {
		t.Errorf("event_id mismatch: %s", events[0].EventID)
	}
}

func TestUnsyncedCount(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := store.AppendEvent(ctx, &storage.Event{
			EventID:       "evt_unsynced_" + string(rune('0'+i)),
			EventType:     "sensor.reading.recorded",
			SchemaVersion: "1.0",
			SiteID:        "s1",
			EdgeID:        "e1",
			RecordedAt:    time.Now().UTC(),
			SourceModule:  "collector",
			PayloadJSON:   `{}`,
			EventJSON:     `{}`,
		})
		if err != nil {
			t.Fatalf("AppendEvent %d: %v", i, err)
		}
	}

	count, err := store.UnsyncedCount(ctx)
	if err != nil {
		t.Fatalf("UnsyncedCount: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 unsynced, got %d", count)
	}
}

func TestKVSetGet(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.KVSet(ctx, "test_key", "test_value"); err != nil {
		t.Fatalf("KVSet: %v", err)
	}
	val, ok, err := store.KVGet(ctx, "test_key")
	if err != nil {
		t.Fatalf("KVGet: %v", err)
	}
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != "test_value" {
		t.Errorf("expected 'test_value', got %q", val)
	}

	// Overwrite
	if err := store.KVSet(ctx, "test_key", "updated"); err != nil {
		t.Fatalf("KVSet update: %v", err)
	}
	val, _, _ = store.KVGet(ctx, "test_key")
	if val != "updated" {
		t.Errorf("expected 'updated', got %q", val)
	}
}

func TestTankEnvironmentUpsertAndList(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	valueOld := 6.1
	valueNew := 6.8

	if err := store.UpsertTankEnvironmentReading(ctx, &storage.CurrentTankEnvironmentReading{
		TankID:      "tank_01",
		Metric:      "dissolved_oxygen",
		Value:       &valueOld,
		Unit:        "mg/L",
		Quality:     "ok",
		SensorID:    "sensor_do_01",
		DeviceID:    "mock_probe_01",
		LastEventID: "evt_old",
		ObservedAt:  "2026-05-03T09:00:00Z",
	}, `{"metric":"dissolved_oxygen","value":6.1}`); err != nil {
		t.Fatalf("UpsertTankEnvironmentReading old: %v", err)
	}
	if err := store.UpsertTankEnvironmentReading(ctx, &storage.CurrentTankEnvironmentReading{
		TankID:      "tank_01",
		Metric:      "dissolved_oxygen",
		Value:       &valueNew,
		Unit:        "mg/L",
		Quality:     "ok",
		SensorID:    "sensor_do_01",
		DeviceID:    "mock_probe_01",
		LastEventID: "evt_new",
		ObservedAt:  "2026-05-03T09:01:00Z",
	}, `{"metric":"dissolved_oxygen","value":6.8}`); err != nil {
		t.Fatalf("UpsertTankEnvironmentReading new: %v", err)
	}

	readings, err := store.ListTankEnvironment(ctx, "tank_01")
	if err != nil {
		t.Fatalf("ListTankEnvironment: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("expected 1 current environment reading, got %d", len(readings))
	}
	if readings[0].LastEventID != "evt_new" || readings[0].Value == nil || *readings[0].Value != valueNew {
		t.Fatalf("expected latest reading projection, got %#v", readings[0])
	}
}

func TestTankProfileUpsertGetAndList(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	minDO := 6.0
	maxCO2 := 12.0

	if err := store.UpsertTankProfile(ctx, &storage.TankProfile{
		TankID:         "tank_01",
		PlatformTankID: "platform-tank-uuid",
		DisplayName:    "RAS Tank 01",
		Species:        "atlantic_salmon",
		SystemType:     "land_based_ras",
		VolumeM3:       25,
		BiomassKg:      1200,
		FishCount:      2400,
		AvgWeightG:     500,
		TargetRanges: []storage.MetricRange{
			{Metric: "dissolved_oxygen", Min: &minDO, Unit: "mg/L"},
			{Metric: "carbon_dioxide", Max: &maxCO2, Unit: "mg/L"},
		},
		Metadata: map[string]any{"mode": "local_first"},
	}); err != nil {
		t.Fatalf("UpsertTankProfile: %v", err)
	}

	profile, err := store.GetTankProfile(ctx, "tank_01")
	if err != nil {
		t.Fatalf("GetTankProfile: %v", err)
	}
	if profile == nil || profile.Species != "atlantic_salmon" || profile.PlatformTankID != "platform-tank-uuid" {
		t.Fatalf("unexpected tank profile: %#v", profile)
	}
	if len(profile.TargetRanges) != 2 || profile.TargetRanges[0].Metric != "dissolved_oxygen" {
		t.Fatalf("target ranges not round-tripped: %#v", profile.TargetRanges)
	}

	profiles, err := store.ListTankProfiles(ctx)
	if err != nil {
		t.Fatalf("ListTankProfiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].TankID != "tank_01" {
		t.Fatalf("unexpected tank profile list: %#v", profiles)
	}

	// C-9 — 물리 정보 round-trip 검증.
	if err := store.UpsertTankProfile(ctx, &storage.TankProfile{
		TankID:      "tank_phys",
		DisplayName: "Round Tank",
		Species:     "atlantic_salmon",
		SystemType:  "land_based_ras",
		FormFactor:  "round",
		DiameterM:   4.5,
		DepthM:      2.0,
		VolumeM3:    31.8,
	}); err != nil {
		t.Fatalf("UpsertTankProfile physical: %v", err)
	}
	physical, err := store.GetTankProfile(ctx, "tank_phys")
	if err != nil || physical == nil {
		t.Fatalf("GetTankProfile physical: %v", err)
	}
	if physical.FormFactor != "round" || physical.DiameterM != 4.5 || physical.DepthM != 2.0 {
		t.Fatalf("physical dimensions not round-tripped: %#v", physical)
	}
}

func TestDeviceStatusUpsertAndListIncludesDetails(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	lastSeenAt := time.Now().UTC()

	if err := store.UpsertDeviceStatus(ctx, "mock_probe_01", "water_quality_sensor", "online", "ok", "evt_device_001", &lastSeenAt, `{"device_id":"mock_probe_01","tank_id":"tank_01","status":"online"}`); err != nil {
		t.Fatalf("UpsertDeviceStatus: %v", err)
	}
	devices, err := store.ListDeviceStatuses(ctx)
	if err != nil {
		t.Fatalf("ListDeviceStatuses: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	device := devices[0]
	if device["device_id"] != "mock_probe_01" || device["last_event_id"] != "evt_device_001" {
		t.Fatalf("unexpected device projection: %#v", device)
	}
	details, ok := device["details"].(map[string]any)
	if !ok || details["tank_id"] != "tank_01" {
		t.Fatalf("expected decoded details with tank_id, got %#v", device["details"])
	}
}

func TestCameraStatusUpsertAndList(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	camera := &storage.CurrentCameraStatus{
		CameraID:       "cam_tank_01_front",
		TankID:         "tank_01",
		Status:         "online",
		IngestFPS:      24.8,
		LastEventID:    "evt_camera_001",
		LastFrameAt:    time.Now().UTC().Format(time.RFC3339Nano),
		ReconnectCount: 1,
		DroppedFrames:  3,
	}
	if err := store.UpsertCameraStatus(ctx, camera, `{"camera_id":"cam_tank_01_front","tank_id":"tank_01","status":"online"}`); err != nil {
		t.Fatalf("UpsertCameraStatus: %v", err)
	}

	cameras, err := store.ListCameraStatuses(ctx, "tank_01")
	if err != nil {
		t.Fatalf("ListCameraStatuses: %v", err)
	}
	if len(cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(cameras))
	}
	if cameras[0].CameraID != "cam_tank_01_front" || cameras[0].Status != "online" || cameras[0].IngestFPS != 24.8 {
		t.Fatalf("unexpected camera projection: %#v", cameras[0])
	}
	if cameras[0].Details["tank_id"] != "tank_01" {
		t.Fatalf("expected decoded details, got %#v", cameras[0].Details)
	}
}

func TestAlertUpsertAndClear(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	a := &storage.OpenAlert{
		AlertID:        "alert_001",
		AlertDedupeKey: "water_quality.low_dissolved_oxygen|tank|tank_01|rule_do_low",
		AlertType:      "water_quality.low_dissolved_oxygen",
		Severity:       "warning",
		SubjectKind:    "tank",
		SubjectID:      "tank_01",
		RuleID:         "rule_do_low",
		Status:         "open",
		RaisedAt:       time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		PayloadJSON:    `{}`,
	}

	created, err := store.UpsertAlert(ctx, a)
	if err != nil {
		t.Fatalf("UpsertAlert: %v", err)
	}
	if !created {
		t.Error("expected alert to be created (new)")
	}

	// Get it
	existing, err := store.GetOpenAlert(ctx, a.AlertDedupeKey)
	if err != nil {
		t.Fatalf("GetOpenAlert: %v", err)
	}
	if existing == nil {
		t.Fatal("expected alert, got nil")
	}
	if existing.AlertType != "water_quality.low_dissolved_oxygen" {
		t.Errorf("unexpected alert_type: %s", existing.AlertType)
	}

	// Upsert again (update)
	created2, _ := store.UpsertAlert(ctx, a)
	if created2 {
		t.Error("second upsert should not count as created")
	}

	// Clear
	if err := store.ClearAlert(ctx, a.AlertDedupeKey); err != nil {
		t.Fatalf("ClearAlert: %v", err)
	}
	gone, _ := store.GetOpenAlert(ctx, a.AlertDedupeKey)
	if gone != nil {
		t.Error("alert should be gone after clear")
	}
}

func TestLatestSensorReadingsByTank(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	eventsToAppend := []*storage.Event{
		{
			EventID:       "evt_tank01_do_old",
			EventType:     "sensor.reading.recorded",
			SchemaVersion: "1.0",
			SiteID:        "site_test",
			EdgeID:        "edge_test",
			RecordedAt:    now.Add(-2 * time.Minute),
			SourceModule:  "collector",
			PayloadJSON:   `{"reading_id":"r1","sensor_id":"sensor_01","device_id":"wq_01","metric":"dissolved_oxygen","value":6.1,"unit":"mg/L","quality":"ok","observed_at":"2026-05-03T09:00:00Z","location":{"tank_id":"tank_01"}}`,
			EventJSON:     `{}`,
		},
		{
			EventID:       "evt_tank01_do_new",
			EventType:     "sensor.reading.recorded",
			SchemaVersion: "1.0",
			SiteID:        "site_test",
			EdgeID:        "edge_test",
			RecordedAt:    now.Add(-1 * time.Minute),
			SourceModule:  "collector",
			PayloadJSON:   `{"reading_id":"r2","sensor_id":"sensor_01","device_id":"wq_01","metric":"dissolved_oxygen","value":6.8,"unit":"mg/L","quality":"ok","observed_at":"2026-05-03T09:01:00Z","location":{"tank_id":"tank_01"}}`,
			EventJSON:     `{}`,
		},
		{
			EventID:       "evt_tank01_ph",
			EventType:     "sensor.reading.recorded",
			SchemaVersion: "1.0",
			SiteID:        "site_test",
			EdgeID:        "edge_test",
			RecordedAt:    now,
			SourceModule:  "collector",
			PayloadJSON:   `{"reading_id":"r3","sensor_id":"sensor_02","device_id":"wq_02","metric":"ph","value":7.5,"unit":"pH","quality":"ok","observed_at":"2026-05-03T09:02:00Z","location":{"tank_id":"tank_01"}}`,
			EventJSON:     `{}`,
		},
		{
			EventID:       "evt_tank02_do",
			EventType:     "sensor.reading.recorded",
			SchemaVersion: "1.0",
			SiteID:        "site_test",
			EdgeID:        "edge_test",
			RecordedAt:    now,
			SourceModule:  "collector",
			PayloadJSON:   `{"reading_id":"r4","sensor_id":"sensor_03","device_id":"wq_03","metric":"dissolved_oxygen","value":5.9,"unit":"mg/L","quality":"ok","observed_at":"2026-05-03T09:02:00Z","location":{"tank_id":"tank_02"}}`,
			EventJSON:     `{}`,
		},
	}
	for _, e := range eventsToAppend {
		if _, err := store.AppendEvent(ctx, e); err != nil {
			t.Fatalf("AppendEvent %s: %v", e.EventID, err)
		}
	}

	readings, err := store.LatestSensorReadings(ctx, storage.LatestReadingFilter{TankID: "tank_01", Limit: 10})
	if err != nil {
		t.Fatalf("LatestSensorReadings: %v", err)
	}
	if len(readings) != 2 {
		t.Fatalf("expected 2 latest metric readings for tank_01, got %d", len(readings))
	}

	byMetric := map[string]*storage.LatestSensorReading{}
	for _, r := range readings {
		byMetric[r.Metric] = r
	}
	if byMetric["dissolved_oxygen"] == nil || byMetric["dissolved_oxygen"].EventID != "evt_tank01_do_new" {
		t.Fatalf("expected latest DO event evt_tank01_do_new, got %#v", byMetric["dissolved_oxygen"])
	}
	if byMetric["ph"] == nil || byMetric["ph"].EventID != "evt_tank01_ph" {
		t.Fatalf("expected latest pH event evt_tank01_ph, got %#v", byMetric["ph"])
	}
}

func TestCommandIdempotency(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	cmd := &storage.ControlCommand{
		CommandID:      "cmd_001",
		IdempotencyKey: "idem_key_001",
		TargetDeviceID: "feeder_01",
		CommandType:    "feed.start",
		Status:         "accepted",
		RequestedAt:    time.Now().UTC(),
		ExpiresAt:      time.Now().UTC().Add(60 * time.Second),
		LastEventID:    "evt_001",
		PayloadJSON:    `{}`,
	}

	if err := store.InsertCommand(ctx, cmd); err != nil {
		t.Fatalf("InsertCommand: %v", err)
	}

	found, err := store.GetCommandByIdempotencyKey(ctx, "idem_key_001")
	if err != nil {
		t.Fatalf("GetCommandByIdempotencyKey: %v", err)
	}
	if found == nil {
		t.Fatal("expected command, got nil")
	}
	if found.CommandID != "cmd_001" {
		t.Errorf("unexpected command_id: %s", found.CommandID)
	}

	// Missing key should return nil
	missing, err := store.GetCommandByIdempotencyKey(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetCommandByIdempotencyKey (missing): %v", err)
	}
	if missing != nil {
		t.Error("expected nil for missing key")
	}
}

func TestWaterQualityBucketProjectionUpsertAndList(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	bucketStart := time.Date(2026, 5, 8, 1, 0, 0, 0, time.UTC)
	do := 8.2
	ph := 7.7
	if err := store.UpsertWaterQualityBucket(ctx, &storage.WaterQualityBucketProjection{
		TankID:           "tank_1",
		BucketStart:      bucketStart,
		BucketSec:        120,
		PHAvg:            &ph,
		DOMgLAvg:         &do,
		Quality:          "ok",
		SampleCount:      3,
		SuspectCount:     0,
		SourceReadingIDs: `["r1","r2"]`,
	}); err != nil {
		t.Fatalf("UpsertWaterQualityBucket: %v", err)
	}
	do = 8.4
	if err := store.UpsertWaterQualityBucket(ctx, &storage.WaterQualityBucketProjection{
		TankID:           "tank_1",
		BucketStart:      bucketStart,
		BucketSec:        120,
		PHAvg:            &ph,
		DOMgLAvg:         &do,
		Quality:          "suspect",
		SampleCount:      4,
		SuspectCount:     1,
		SourceReadingIDs: `["r1","r2","r3"]`,
	}); err != nil {
		t.Fatalf("second UpsertWaterQualityBucket: %v", err)
	}

	got, err := store.ListWaterQualityBuckets(ctx, "tank_1", nil, nil, 10)
	if err != nil {
		t.Fatalf("ListWaterQualityBuckets: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].DOMgLAvg == nil || *got[0].DOMgLAvg != 8.4 || got[0].Quality != "suspect" || got[0].SampleCount != 4 {
		t.Fatalf("unexpected bucket: %#v", got[0])
	}
}

func TestFeedingImpactAnalysisProjectionUpsertAndGet(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	fedAt := time.Date(2026, 5, 8, 1, 30, 0, 0, time.UTC)
	drop := 0.5
	recovery := 42
	if err := store.UpsertFeedingImpactAnalysis(ctx, &storage.FeedingImpactAnalysisProjection{
		AnalysisID:      "feed_wq_feeding_1",
		FeedingID:       "feeding_1",
		TankID:          "tank_1",
		FeedAmountG:     450,
		FedAt:           fedAt,
		DODropMgL:       &drop,
		DORecoveryMin:   &recovery,
		Quality:         "ok",
		ReasonCodesJSON: `["do_drop_computed"]`,
	}); err != nil {
		t.Fatalf("UpsertFeedingImpactAnalysis: %v", err)
	}
	drop = 0.4
	if err := store.UpsertFeedingImpactAnalysis(ctx, &storage.FeedingImpactAnalysisProjection{
		AnalysisID:      "feed_wq_feeding_1",
		FeedingID:       "feeding_1",
		TankID:          "tank_1",
		FeedAmountG:     450,
		FedAt:           fedAt,
		DODropMgL:       &drop,
		DORecoveryMin:   &recovery,
		Quality:         "suspect",
		ReasonCodesJSON: `["degraded_input_quality"]`,
	}); err != nil {
		t.Fatalf("second UpsertFeedingImpactAnalysis: %v", err)
	}

	got, err := store.GetFeedingImpactAnalysis(ctx, "feeding_1")
	if err != nil {
		t.Fatalf("GetFeedingImpactAnalysis: %v", err)
	}
	if got == nil || got.DODropMgL == nil || *got.DODropMgL != 0.4 || got.Quality != "suspect" {
		t.Fatalf("unexpected analysis: %#v", got)
	}
}

func TestCameraProfileRegistryPersistsTankCameraWithoutPassword(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	profile := &storage.CameraProfile{
		CameraID:          "camera_tank_01_side",
		TankID:            "tank_01",
		DisplayName:       "Tank 01 underwater side",
		Vendor:            "hikvision",
		Host:              "192.168.0.94",
		RTSPPort:          554,
		HTTPPort:          80,
		Username:          "admin",
		PasswordSecretRef: "secret://camera/camera_tank_01_side/password",
		Position:          "underwater_side",
		Purpose:           []string{"operator_view", "vision_ai"},
		StreamProfiles: map[string]any{
			"main": map[string]any{"path": "/Streaming/Channels/101", "codec": "h264"},
			"sub":  map[string]any{"path": "/Streaming/Channels/102", "codec": "h264"},
		},
		ClipPolicy: map[string]any{"feeding_clip_before_sec": float64(10), "feeding_clip_after_sec": float64(120)},
		Status:     "configured",
	}
	if err := store.UpsertCameraProfile(ctx, profile); err != nil {
		t.Fatalf("UpsertCameraProfile: %v", err)
	}

	got, err := store.GetCameraProfile(ctx, "camera_tank_01_side")
	if err != nil {
		t.Fatalf("GetCameraProfile: %v", err)
	}
	if got == nil {
		t.Fatal("expected camera profile")
	}
	if got.TankID != "tank_01" || got.Host != "192.168.0.94" || got.Status != "configured" {
		t.Fatalf("unexpected profile: %+v", got)
	}
	if got.PasswordSecretRef == "" {
		t.Fatal("expected password secret reference, not plaintext password")
	}
	if got.StreamProfiles["main"] == nil || got.StreamProfiles["sub"] == nil {
		t.Fatalf("expected main/sub stream profiles: %+v", got.StreamProfiles)
	}

	items, err := store.ListCameraProfiles(ctx, "tank_01")
	if err != nil {
		t.Fatalf("ListCameraProfiles: %v", err)
	}
	if len(items) != 1 || items[0].CameraID != "camera_tank_01_side" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

// C-12 — 새 메타 컬럼 round-trip.
func TestCameraProfileC12ViewGeometryRoundTrip(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	heightAboveWater := 2.5
	tilt := 90.0
	profile := &storage.CameraProfile{
		CameraID:         "cam_tank_02_top",
		TankID:           "tank_02",
		DisplayName:      "Tank 02 top-down (counting)",
		Vendor:           "hikvision",
		Status:           "configured",
		MountLocation:    "feeder_zone",
		ViewAngle:        "top_down",
		HeightFromWaterM: &heightAboveWater,
		TiltDeg:          &tilt,
		Purpose:          []string{"counting", "feeding_detect"},
	}
	if err := store.UpsertCameraProfile(ctx, profile); err != nil {
		t.Fatalf("UpsertCameraProfile: %v", err)
	}
	got, err := store.GetCameraProfile(ctx, "cam_tank_02_top")
	if err != nil || got == nil {
		t.Fatalf("GetCameraProfile: %v / %+v", err, got)
	}
	if got.MountLocation != "feeder_zone" || got.ViewAngle != "top_down" {
		t.Fatalf("mount/view round-trip: %+v", got)
	}
	if got.HeightFromWaterM == nil || *got.HeightFromWaterM != 2.5 {
		t.Fatalf("height_from_water_m round-trip: %+v", got.HeightFromWaterM)
	}
	if got.TiltDeg == nil || *got.TiltDeg != 90.0 {
		t.Fatalf("tilt_deg round-trip: %+v", got.TiltDeg)
	}

	// 수중 음수 깊이도 검증.
	depth := -1.2
	underwaterProfile := &storage.CameraProfile{
		CameraID:         "cam_tank_02_uw",
		TankID:           "tank_02",
		DisplayName:      "Tank 02 underwater stereo (size)",
		Status:           "configured",
		MountLocation:    "tank_side",
		ViewAngle:        "underwater_side",
		HeightFromWaterM: &depth,
		Purpose:          []string{"size_estimation", "behavior"},
	}
	if err := store.UpsertCameraProfile(ctx, underwaterProfile); err != nil {
		t.Fatalf("UpsertCameraProfile underwater: %v", err)
	}
	got2, err := store.GetCameraProfile(ctx, "cam_tank_02_uw")
	if err != nil || got2 == nil {
		t.Fatalf("GetCameraProfile underwater: %v / %+v", err, got2)
	}
	if got2.HeightFromWaterM == nil || *got2.HeightFromWaterM != -1.2 {
		t.Fatalf("underwater depth (negative) round-trip: %+v", got2.HeightFromWaterM)
	}
}

func TestTankAutonomousModeUpsertGetList(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// 1. 없는 Cage/Tank → (nil, nil)
	got, err := store.GetTankAutonomousMode(ctx, "tank_x")
	if err != nil {
		t.Fatalf("GetTankAutonomousMode missing: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for unknown tank, got row")
	}

	// 2. upsert
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.UpsertTankAutonomousMode(ctx, &storage.TankAutonomousMode{
		TankID:    "tank_01",
		Mode:      "observation",
		Reason:    "AI 학습 기간 시작",
		ChangedAt: now,
		ChangedBy: "operator",
	}); err != nil {
		t.Fatalf("UpsertTankAutonomousMode: %v", err)
	}

	// 3. get
	row, err := store.GetTankAutonomousMode(ctx, "tank_01")
	if err != nil {
		t.Fatalf("GetTankAutonomousMode: %v", err)
	}
	if row == nil || row.Mode != "observation" || row.Reason != "AI 학습 기간 시작" || row.ChangedBy != "operator" {
		t.Fatalf("unexpected row: %+v", row)
	}

	// 4. upsert 갱신
	if err := store.UpsertTankAutonomousMode(ctx, &storage.TankAutonomousMode{
		TankID:    "tank_01",
		Mode:      "full",
		Reason:    "자율 전환",
		ChangedAt: now.Add(time.Minute),
		ChangedBy: "admin",
	}); err != nil {
		t.Fatalf("UpsertTankAutonomousMode update: %v", err)
	}
	updated, _ := store.GetTankAutonomousMode(ctx, "tank_01")
	if updated.Mode != "full" || updated.ChangedBy != "admin" {
		t.Fatalf("expected updated row: %+v", updated)
	}

	// 5. 두 번째 Cage/Tank upsert + list
	if err := store.UpsertTankAutonomousMode(ctx, &storage.TankAutonomousMode{
		TankID:    "tank_02",
		Mode:      "off",
		ChangedAt: now,
		ChangedBy: "operator",
	}); err != nil {
		t.Fatalf("UpsertTankAutonomousMode tank_02: %v", err)
	}
	all, err := store.ListTankAutonomousModes(ctx)
	if err != nil {
		t.Fatalf("ListTankAutonomousModes: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(all))
	}
}

// TestTankLifecycleUpsertGet — lifecycle upsert/get 기본 경로 + unknown tank nil 반환.
func TestTankLifecycleUpsertGet(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	hw := 500.0

	// 1. unknown tank → nil
	got, err := store.GetTankLifecycle(ctx, "tank_unknown")
	if err != nil {
		t.Fatalf("GetTankLifecycle unknown: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unknown tank, got %+v", got)
	}

	// 2. upsert active
	lc := &storage.TankLifecycle{
		TankID:               "tank_01",
		ActiveStockingID:     "stocking_abc",
		Species:              "연어",
		GrowthStage:          "growout",
		InitialCount:         1000,
		InitialAvgWeightG:    50.0,
		TargetHarvestWeightG: &hw,
		TargetHarvestDate:    "2026-10-01",
		SourceHatchery:       "동해수산",
		StockedAt:            now,
		Status:               "active",
		UpdatedAt:            now,
	}
	if err := store.UpsertTankLifecycle(ctx, lc); err != nil {
		t.Fatalf("UpsertTankLifecycle active: %v", err)
	}

	// 3. get → matches
	got2, err := store.GetTankLifecycle(ctx, "tank_01")
	if err != nil {
		t.Fatalf("GetTankLifecycle after upsert: %v", err)
	}
	if got2 == nil {
		t.Fatal("expected non-nil lifecycle after upsert")
	}
	if got2.ActiveStockingID != "stocking_abc" {
		t.Errorf("stocking_id: got %q want %q", got2.ActiveStockingID, "stocking_abc")
	}
	if got2.Status != "active" {
		t.Errorf("status: got %q want active", got2.Status)
	}
	if got2.InitialCount != 1000 {
		t.Errorf("initial_count: got %d want 1000", got2.InitialCount)
	}
	if got2.TargetHarvestWeightG == nil || *got2.TargetHarvestWeightG != 500.0 {
		t.Errorf("target_harvest_weight_g: got %v want 500.0", got2.TargetHarvestWeightG)
	}

	// 4. upsert status=harvested
	lc.Status = "harvested"
	lc.UpdatedAt = now.Add(time.Hour)
	if err := store.UpsertTankLifecycle(ctx, lc); err != nil {
		t.Fatalf("UpsertTankLifecycle harvested: %v", err)
	}
	got3, err := store.GetTankLifecycle(ctx, "tank_01")
	if err != nil {
		t.Fatalf("GetTankLifecycle harvested: %v", err)
	}
	if got3 == nil || got3.Status != "harvested" {
		t.Errorf("expected status=harvested, got %+v", got3)
	}
}

// TestTankSamplingUpsertGet — sampling projection 기본 동작 검증.
func TestTankSamplingUpsertGet(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// 1. 행 없으면 nil, nil
	got0, err := store.GetTankSampling(ctx, "tank_01")
	if err != nil {
		t.Fatalf("GetTankSampling missing: %v", err)
	}
	if got0 != nil {
		t.Fatalf("expected nil, got %+v", got0)
	}

	// 2. upsert
	score := 8
	std := 10.5
	minW := 45.0
	maxW := 80.0
	abnormal := 2
	ts := &storage.TankSampling{
		TankID:           "tank_01",
		LatestSamplingID: "sampling_001",
		StockingID:       "stocking_abc",
		SampledCount:     30,
		AvgWeightG:       60.0,
		StdWeightG:       &std,
		MinWeightG:       &minW,
		MaxWeightG:       &maxW,
		HealthScore:      &score,
		HealthNotes:      "양호",
		AbnormalCount:    &abnormal,
		SampledAt:        now,
		RecordedBy:       "op1",
		UpdatedAt:        now,
	}
	if err := store.UpsertTankSampling(ctx, ts); err != nil {
		t.Fatalf("UpsertTankSampling: %v", err)
	}

	// 3. 조회
	got, err := store.GetTankSampling(ctx, "tank_01")
	if err != nil {
		t.Fatalf("GetTankSampling: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil sampling after upsert")
	}
	if got.LatestSamplingID != "sampling_001" {
		t.Errorf("sampling_id: got %q", got.LatestSamplingID)
	}
	if got.SampledCount != 30 {
		t.Errorf("sampled_count: got %d", got.SampledCount)
	}
	if got.AvgWeightG != 60.0 {
		t.Errorf("avg_weight_g: got %f", got.AvgWeightG)
	}
	if got.HealthScore == nil || *got.HealthScore != 8 {
		t.Errorf("health_score: got %v", got.HealthScore)
	}
	if got.AbnormalCount == nil || *got.AbnormalCount != 2 {
		t.Errorf("abnormal_count: got %v", got.AbnormalCount)
	}
	if got.StdWeightG == nil || *got.StdWeightG != 10.5 {
		t.Errorf("std_weight_g: got %v", got.StdWeightG)
	}

	// 4. upsert (덮어쓰기)
	ts.LatestSamplingID = "sampling_002"
	ts.AvgWeightG = 65.0
	ts.StdWeightG = nil
	ts.StockingID = ""
	if err := store.UpsertTankSampling(ctx, ts); err != nil {
		t.Fatalf("UpsertTankSampling overwrite: %v", err)
	}
	got2, err := store.GetTankSampling(ctx, "tank_01")
	if err != nil {
		t.Fatalf("GetTankSampling overwrite: %v", err)
	}
	if got2.LatestSamplingID != "sampling_002" {
		t.Errorf("expected sampling_002, got %q", got2.LatestSamplingID)
	}
	if got2.AvgWeightG != 65.0 {
		t.Errorf("avg_weight_g overwrite: got %f", got2.AvgWeightG)
	}
	if got2.StdWeightG != nil {
		t.Errorf("std_weight_g should be nil after overwrite, got %v", got2.StdWeightG)
	}
	if got2.StockingID != "" {
		t.Errorf("stocking_id should be empty, got %q", got2.StockingID)
	}
}

func TestTankFCRCalibrationUpsertGet(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	calibratedAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	// 최초 upsert
	c := &storage.TankFCRCalibration{
		TankID:          "tank_fcr_01",
		StockingID:      "stocking_fcr_01",
		SamplingID:      "sampling_fcr_01",
		DefaultFCR:      1.4,
		ObservedFCR:     1.25,
		CalibratedFCR:   1.25,
		DeviationPct:    -10.71,
		CumulativeFeedG: 10000.0,
		DeltaBiomassG:   8000.0,
		CalibratedAt:    calibratedAt,
	}
	if err := store.UpsertTankFCRCalibration(ctx, c); err != nil {
		t.Fatalf("UpsertTankFCRCalibration: %v", err)
	}

	got, err := store.GetTankFCRCalibration(ctx, "tank_fcr_01")
	if err != nil {
		t.Fatalf("GetTankFCRCalibration: %v", err)
	}
	if got == nil {
		t.Fatal("expected calibration row, got nil")
	}
	if got.StockingID != "stocking_fcr_01" {
		t.Errorf("stocking_id: got %q", got.StockingID)
	}
	if got.CalibratedFCR != 1.25 {
		t.Errorf("calibrated_fcr: got %f, want 1.25", got.CalibratedFCR)
	}
	if got.DefaultFCR != 1.4 {
		t.Errorf("default_fcr: got %f, want 1.4", got.DefaultFCR)
	}
	if got.CalibratedAt.UTC() != calibratedAt.UTC() {
		t.Errorf("calibrated_at: got %v, want %v", got.CalibratedAt, calibratedAt)
	}

	// 존재하지 않는 tank → nil
	none, err := store.GetTankFCRCalibration(ctx, "no_such_tank")
	if err != nil {
		t.Fatalf("GetTankFCRCalibration missing: %v", err)
	}
	if none != nil {
		t.Errorf("expected nil for missing tank, got %+v", none)
	}

	// upsert 으로 덮어쓰기 확인
	c2 := *c
	c2.CalibratedFCR = 1.30
	c2.SamplingID = "sampling_fcr_02"
	if err := store.UpsertTankFCRCalibration(ctx, &c2); err != nil {
		t.Fatalf("UpsertTankFCRCalibration overwrite: %v", err)
	}
	got2, err := store.GetTankFCRCalibration(ctx, "tank_fcr_01")
	if err != nil {
		t.Fatalf("GetTankFCRCalibration overwrite: %v", err)
	}
	if got2.CalibratedFCR != 1.30 {
		t.Errorf("overwrite calibrated_fcr: got %f, want 1.30", got2.CalibratedFCR)
	}
	if got2.SamplingID != "sampling_fcr_02" {
		t.Errorf("overwrite sampling_id: got %q", got2.SamplingID)
	}
}

func TestTankDecisionPolicyUpsertGet(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// 행 없음 → nil
	got, err := store.GetTankDecisionPolicy(ctx, "tank_policy_01")
	if err != nil {
		t.Fatalf("GetTankDecisionPolicy empty: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing tank, got %+v", got)
	}

	now := time.Now().UTC().Truncate(time.Second)
	p := &storage.TankDecisionPolicy{
		TankID:             "tank_policy_01",
		AutoExecuteEnabled: true,
		GraceMinutes:       15,
		UpdatedAt:          now,
		UpdatedBy:          "operator_test",
	}
	if err := store.UpsertTankDecisionPolicy(ctx, p); err != nil {
		t.Fatalf("UpsertTankDecisionPolicy: %v", err)
	}

	got, err = store.GetTankDecisionPolicy(ctx, "tank_policy_01")
	if err != nil {
		t.Fatalf("GetTankDecisionPolicy after upsert: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil after upsert")
	}
	if got.AutoExecuteEnabled != true {
		t.Errorf("auto_execute_enabled: got %v, want true", got.AutoExecuteEnabled)
	}
	if got.GraceMinutes != 15 {
		t.Errorf("grace_minutes: got %d, want 15", got.GraceMinutes)
	}
	if got.UpdatedBy != "operator_test" {
		t.Errorf("updated_by: got %q", got.UpdatedBy)
	}

	// 덮어쓰기 확인
	p2 := *p
	p2.AutoExecuteEnabled = false
	p2.GraceMinutes = 5
	if err := store.UpsertTankDecisionPolicy(ctx, &p2); err != nil {
		t.Fatalf("UpsertTankDecisionPolicy overwrite: %v", err)
	}
	got2, err := store.GetTankDecisionPolicy(ctx, "tank_policy_01")
	if err != nil {
		t.Fatalf("GetTankDecisionPolicy overwrite: %v", err)
	}
	if got2.AutoExecuteEnabled != false {
		t.Errorf("overwrite auto_execute_enabled: got %v, want false", got2.AutoExecuteEnabled)
	}
	if got2.GraceMinutes != 5 {
		t.Errorf("overwrite grace_minutes: got %d, want 5", got2.GraceMinutes)
	}
}

func TestTankWeightHistoryUpsertList(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// 1. 빈 상태에서 조회 → 빈 slice
	snaps, err := store.ListTankWeightHistory(ctx, "tank_wh_01", 30)
	if err != nil {
		t.Fatalf("ListTankWeightHistory empty: %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("expected 0 snaps, got %d", len(snaps))
	}

	// 2. upsert
	snap := &storage.TankWeightSnapshot{
		TankID:              "tank_wh_01",
		SnapshotDate:        "2026-04-20",
		EstimatedAvgWeightG: 65.3,
		AnchorWeightG:       50.0,
		AnchorSource:        "stocking",
		DaysSinceAnchor:     15,
		ExpectedFCR:         1.4,
		FCRSource:           "default",
		CumulativeFeedG:     8400.0,
		Quality:             "ok",
		SnapshotAt:          now,
	}
	if err := store.UpsertTankWeightSnapshot(ctx, snap); err != nil {
		t.Fatalf("UpsertTankWeightSnapshot: %v", err)
	}

	// 3. 조회 (365일 범위)
	snaps, err = store.ListTankWeightHistory(ctx, "tank_wh_01", 365)
	if err != nil {
		t.Fatalf("ListTankWeightHistory after upsert: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snap, got %d", len(snaps))
	}
	s := snaps[0]
	if s.TankID != "tank_wh_01" {
		t.Errorf("tank_id: got %q", s.TankID)
	}
	if s.SnapshotDate != "2026-04-20" {
		t.Errorf("snapshot_date: got %q", s.SnapshotDate)
	}
	if s.EstimatedAvgWeightG != 65.3 {
		t.Errorf("estimated_avg_weight_g: got %v", s.EstimatedAvgWeightG)
	}
	if s.FCRSource != "default" {
		t.Errorf("fcr_source: got %q", s.FCRSource)
	}
	if s.SnapshotAt.IsZero() {
		t.Error("snapshot_at should not be zero")
	}

	// 4. 같은 날 UPSERT → 덮어쓰기 (마지막 값)
	snap2 := *snap // snap is *storage.TankWeightSnapshot
	snap2.EstimatedAvgWeightG = 67.1
	snap2.FCRSource = "calibrated"
	if err := store.UpsertTankWeightSnapshot(ctx, &snap2); err != nil {
		t.Fatalf("UpsertTankWeightSnapshot overwrite: %v", err)
	}
	snaps2, err := store.ListTankWeightHistory(ctx, "tank_wh_01", 365)
	if err != nil {
		t.Fatalf("ListTankWeightHistory overwrite: %v", err)
	}
	if len(snaps2) != 1 {
		t.Fatalf("expected still 1 snap after upsert, got %d", len(snaps2))
	}
	if snaps2[0].EstimatedAvgWeightG != 67.1 {
		t.Errorf("overwrite: expected 67.1, got %v", snaps2[0].EstimatedAvgWeightG)
	}
	if snaps2[0].FCRSource != "calibrated" {
		t.Errorf("overwrite fcr_source: got %q", snaps2[0].FCRSource)
	}

	// 5. 다른 날 추가
	snap3 := *snap // storage.TankWeightSnapshot value copy
	snap3.SnapshotDate = "2026-04-21"
	snap3.EstimatedAvgWeightG = 68.5
	if err := store.UpsertTankWeightSnapshot(ctx, &snap3); err != nil {
		t.Fatalf("UpsertTankWeightSnapshot second day: %v", err)
	}
	snaps3, err := store.ListTankWeightHistory(ctx, "tank_wh_01", 365)
	if err != nil {
		t.Fatalf("ListTankWeightHistory two days: %v", err)
	}
	if len(snaps3) != 2 {
		t.Fatalf("expected 2 snaps, got %d", len(snaps3))
	}
	// ASC 정렬 확인
	if snaps3[0].SnapshotDate >= snaps3[1].SnapshotDate {
		t.Errorf("expected ASC order: %q >= %q", snaps3[0].SnapshotDate, snaps3[1].SnapshotDate)
	}
}
