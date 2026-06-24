package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bluei.kr/edge/internal/actuator"
	"bluei.kr/edge/internal/controller"
	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/sensor"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/species"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/wtg"
)

// domainMigSQL — migSQL + 009~013 schema 추가 (테스트 전용 인라인 migration).
const domainMigSQL = migSQL + `
CREATE TABLE IF NOT EXISTS farms (
  farm_id            TEXT PRIMARY KEY,
  license_no         TEXT NOT NULL DEFAULT '',
  operator           TEXT NOT NULL DEFAULT '',
  certifications_json TEXT NOT NULL DEFAULT '[]',
  metadata_json      TEXT NOT NULL DEFAULT '{}',
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sites (
  site_id       TEXT PRIMARY KEY,
  farm_id       TEXT NOT NULL,
  site_type     TEXT NOT NULL,
  name          TEXT NOT NULL DEFAULT '',
  timezone      TEXT NOT NULL DEFAULT 'Asia/Seoul',
  address       TEXT,
  lat           REAL,
  lon           REAL,
  heading_deg   REAL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sites_marine_gps (
  site_id    TEXT NOT NULL,
  position   TEXT NOT NULL,
  lat        REAL NOT NULL,
  lon        REAL NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (site_id, position)
);
CREATE TABLE IF NOT EXISTS water_treatment_groups (
  wtg_id                   TEXT PRIMARY KEY,
  site_id                  TEXT NOT NULL,
  name                     TEXT NOT NULL DEFAULT '',
  shared_equipment_json    TEXT NOT NULL DEFAULT '{}',
  intake_sensor_id         TEXT,
  outlet_sensor_id         TEXT,
  volume_m3                REAL,
  nh3_processing_kg_per_h  REAL,
  flow_rate_m3_per_h       REAL,
  feeding_policy_json      TEXT NOT NULL DEFAULT '{}',
  created_at               TEXT NOT NULL,
  updated_at               TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS controllers (
  controller_id      TEXT PRIMARY KEY,
  tank_id            TEXT,
  site_id            TEXT,
  actuator_id        TEXT,
  mac_address        TEXT NOT NULL,
  ip_address         TEXT,
  firmware_version   TEXT NOT NULL DEFAULT '',
  status             TEXT NOT NULL DEFAULT 'pending',
  registered_at      TEXT NOT NULL,
  last_seen_at       TEXT,
  commissioning_json TEXT NOT NULL DEFAULT '{}',
  metadata_json      TEXT NOT NULL DEFAULT '{}',
  updated_at         TEXT NOT NULL,
  UNIQUE (mac_address)
);
CREATE TABLE IF NOT EXISTS actuators (
  device_id          TEXT PRIMARY KEY,
  device_type        TEXT NOT NULL,
  site_id            TEXT,
  tank_id            TEXT,
  wtg_id             TEXT,
  controller_id      TEXT,
  model              TEXT NOT NULL DEFAULT '',
  rated_power_w      REAL,
  position_json      TEXT NOT NULL DEFAULT '{}',
  capabilities_json  TEXT NOT NULL DEFAULT '[]',
  metadata_json      TEXT NOT NULL DEFAULT '{}',
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL,
  model_id                 TEXT,
  mount_location           TEXT,
  safety_role_json         TEXT,
  operating_mode           TEXT DEFAULT 'auto',
  alarm_thresholds_json    TEXT,
  last_maintenance_at      TEXT,
  next_maintenance_due_at  TEXT
);
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
CREATE TABLE IF NOT EXISTS sensors (
  sensor_id          TEXT PRIMARY KEY,
  sensor_type        TEXT NOT NULL,
  site_id            TEXT,
  tank_id            TEXT,
  wtg_id             TEXT,
  position           TEXT NOT NULL DEFAULT '',
  hardware           TEXT NOT NULL DEFAULT '',
  capabilities_json  TEXT NOT NULL DEFAULT '[]',
  metadata_json      TEXT NOT NULL DEFAULT '{}',
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL,
  model_id              TEXT,
  mount_location        TEXT,
  installed_depth_m     REAL,
  measurement_role_json TEXT,
  calibration_last_at   TEXT,
  calibration_due_at    TEXT
);
CREATE TABLE IF NOT EXISTS sensor_models (
  model_id TEXT PRIMARY KEY,
  vendor TEXT NOT NULL,
  product_code TEXT NOT NULL,
  display_name TEXT NOT NULL,
  measurement_type TEXT NOT NULL,
  unit TEXT NOT NULL,
  range_min REAL,
  range_max REAL,
  accuracy_value REAL,
  accuracy_unit TEXT,
  response_time_s REAL,
  protocol TEXT,
  calibration_interval_days INTEGER,
  wet_dry TEXT,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS species_profiles (
  species               TEXT PRIMARY KEY,
  display_name          TEXT NOT NULL DEFAULT '',
  lifecycle_stages_json TEXT NOT NULL DEFAULT '{}',
  waste_model_json      TEXT NOT NULL DEFAULT '{}',
  source                TEXT NOT NULL DEFAULT 'default',
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL,
  fao_asfis_code        TEXT,
  scientific_name       TEXT
);
`

func openDomainTestStore(t *testing.T) storage.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "domain_test.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sqlFile := filepath.Join(dir, "domain_mig.sql")
	if err := os.WriteFile(sqlFile, []byte(domainMigSQL), 0o600); err != nil {
		t.Fatalf("write sql: %v", err)
	}
	if err := storage.Migrate(store, sqlFile); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestUpsertFarm(t *testing.T) {
	store := openDomainTestStore(t)
	ctx := context.Background()

	f := &farm.Farm{
		FarmID:         "farm_test_01",
		LicenseNo:      "LIC-001",
		Operator:       "TestOp",
		Certifications: []string{"asc"},
		Sites:          []string{"site_01"},
	}
	if err := store.UpsertFarm(ctx, f); err != nil {
		t.Fatalf("UpsertFarm: %v", err)
	}
	// Idempotent upsert — no error on second call
	if err := store.UpsertFarm(ctx, f); err != nil {
		t.Fatalf("UpsertFarm (idempotent): %v", err)
	}
}

func TestUpsertSiteLand(t *testing.T) {
	store := openDomainTestStore(t)
	ctx := context.Background()

	sl := &site.SiteLand{
		SiteID:   "site_land_test",
		FarmID:   "farm_001",
		Name:     "Test RAS",
		Timezone: "Asia/Seoul",
		Location: site.LandLocation{
			Address: "Test address",
			Coordinates: &site.Coordinates{
				Lat: 37.5,
				Lon: 127.0,
			},
		},
	}
	if err := store.UpsertSiteLand(ctx, sl); err != nil {
		t.Fatalf("UpsertSiteLand: %v", err)
	}
	// Idempotent
	if err := store.UpsertSiteLand(ctx, sl); err != nil {
		t.Fatalf("UpsertSiteLand (idempotent): %v", err)
	}
}

func TestUpsertSiteMarine(t *testing.T) {
	store := openDomainTestStore(t)
	ctx := context.Background()

	sm := &site.SiteMarine{
		SiteID:   "site_marine_test",
		FarmID:   "farm_001",
		Name:     "Test Marine",
		Timezone: "Asia/Seoul",
		Location: site.MarineLocation{
			GPSPoints: []site.MarineGPSPoint{
				{Position: "north", Lat: 34.8, Lon: 128.4, UpdatedAt: "2026-05-01T00:00:00Z"},
				{Position: "south", Lat: 34.79, Lon: 128.4, UpdatedAt: "2026-05-01T00:00:00Z"},
			},
			HeadingDeg: 180,
		},
	}
	if err := store.UpsertSiteMarine(ctx, sm); err != nil {
		t.Fatalf("UpsertSiteMarine: %v", err)
	}
	// Idempotent — GPS points are bulk-replaced
	if err := store.UpsertSiteMarine(ctx, sm); err != nil {
		t.Fatalf("UpsertSiteMarine (idempotent): %v", err)
	}
}

func TestUpsertWTG(t *testing.T) {
	store := openDomainTestStore(t)
	ctx := context.Background()

	g := &wtg.Group{
		WTGID:   "wtg_test_01",
		SiteID:  "site_01",
		Name:    "Test WTG",
		TankIDs: []string{"tank_01", "tank_02"},
		Capacity: wtg.Capacity{
			VolumeM3:            50,
			NH3ProcessingKgPerH: 0.5,
			FlowRateM3PerH:      25,
		},
	}
	if err := store.UpsertWTG(ctx, g); err != nil {
		t.Fatalf("UpsertWTG: %v", err)
	}
	if err := store.UpsertWTG(ctx, g); err != nil {
		t.Fatalf("UpsertWTG (idempotent): %v", err)
	}
}

func TestUpsertController(t *testing.T) {
	store := openDomainTestStore(t)
	ctx := context.Background()

	c := &controller.Controller{
		ControllerID:    "ctrl_test_01",
		MACAddress:      "94:B9:7E:C2:F1:3C",
		FirmwareVersion: "v0.1.0",
		Status:          controller.StatusPending,
		RegisteredAt:    "2026-05-01T00:00:00Z",
	}
	if err := store.UpsertController(ctx, c); err != nil {
		t.Fatalf("UpsertController: %v", err)
	}
	if err := store.UpsertController(ctx, c); err != nil {
		t.Fatalf("UpsertController (idempotent): %v", err)
	}
}

func TestUpsertActuator(t *testing.T) {
	store := openDomainTestStore(t)
	ctx := context.Background()

	a := &actuator.Actuator{
		DeviceID:     "feeder_test_01",
		DeviceType:   "feeder",
		TankID:       "tank_01",
		Capabilities: []string{"feed.start", "feed.stop"},
	}
	if err := store.UpsertActuator(ctx, a); err != nil {
		t.Fatalf("UpsertActuator: %v", err)
	}
	if err := store.UpsertActuator(ctx, a); err != nil {
		t.Fatalf("UpsertActuator (idempotent): %v", err)
	}
}

func TestUpsertSensor(t *testing.T) {
	store := openDomainTestStore(t)
	ctx := context.Background()

	sen := &sensor.Sensor{
		SensorID:     "wq_test_01",
		SensorType:   "water_quality",
		SiteID:       "site_01",
		Position:     "in_tank",
		Capabilities: []string{"temperature", "ph"},
	}
	if err := store.UpsertSensor(ctx, sen); err != nil {
		t.Fatalf("UpsertSensor: %v", err)
	}
	if err := store.UpsertSensor(ctx, sen); err != nil {
		t.Fatalf("UpsertSensor (idempotent): %v", err)
	}
}

func TestUpsertSpeciesProfile(t *testing.T) {
	store := openDomainTestStore(t)
	ctx := context.Background()

	p := &species.Profile{
		DisplayName: "대서양연어",
		LifecycleStages: map[string]species.LifecycleStage{
			"growout": {
				FCRTarget:    1.1,
				WeightRangeG: [2]float64{500, 5000},
				FeedType:     "growout_pellet",
			},
		},
		WasteModel: species.WasteModel{
			ExcretionRatio:              0.30,
			NH3ExcretionPerKgFeed:       35,
			PExcretionPerKgFeed:         8,
			TypicalConsumptionWindowSec: 60,
		},
		Source: "default",
	}
	if err := store.UpsertSpeciesProfile(ctx, "atlantic_salmon", p); err != nil {
		t.Fatalf("UpsertSpeciesProfile: %v", err)
	}
	if err := store.UpsertSpeciesProfile(ctx, "atlantic_salmon", p); err != nil {
		t.Fatalf("UpsertSpeciesProfile (idempotent): %v", err)
	}
}
