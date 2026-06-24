-- Phase 1 initial schema
-- All timestamps are ISO 8601 UTC strings.
-- No foreign keys in Phase 1 skeleton; rely on transaction ordering and unique constraints.

PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS events (
  sequence         INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id         TEXT NOT NULL UNIQUE,
  event_type       TEXT NOT NULL,
  schema_version   TEXT NOT NULL,
  site_id          TEXT NOT NULL,
  edge_id          TEXT NOT NULL,
  recorded_at      TEXT NOT NULL,
  source_module    TEXT NOT NULL,
  source_adapter   TEXT,
  source_device_id TEXT,
  payload_json     TEXT NOT NULL,
  event_json       TEXT NOT NULL,
  correlation_id   TEXT,
  causation_id     TEXT,
  synced_at        TEXT,
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_events_type_time     ON events(event_type, recorded_at);
CREATE INDEX IF NOT EXISTS idx_events_unsynced      ON events(sequence) WHERE synced_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_events_source_device ON events(source_device_id, recorded_at);

CREATE TABLE IF NOT EXISTS current_device_status (
  device_id    TEXT PRIMARY KEY,
  device_type  TEXT NOT NULL,
  status       TEXT NOT NULL,
  health       TEXT NOT NULL,
  last_event_id TEXT NOT NULL,
  last_seen_at  TEXT,
  updated_at    TEXT NOT NULL,
  details_json  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS current_tank_environment (
  tank_id          TEXT NOT NULL,
  metric           TEXT NOT NULL,
  value            REAL,
  unit             TEXT NOT NULL,
  quality          TEXT NOT NULL,
  sensor_id        TEXT NOT NULL,
  device_id        TEXT NOT NULL,
  last_event_id    TEXT NOT NULL,
  observed_at      TEXT NOT NULL,
  updated_at       TEXT NOT NULL,
  payload_json     TEXT NOT NULL,
  PRIMARY KEY (tank_id, metric)
);

CREATE INDEX IF NOT EXISTS idx_current_tank_environment_tank ON current_tank_environment(tank_id);

CREATE TABLE IF NOT EXISTS current_camera_status (
  camera_id       TEXT PRIMARY KEY,
  tank_id         TEXT,
  status          TEXT NOT NULL,
  ingest_fps      REAL NOT NULL,
  last_event_id   TEXT NOT NULL,
  last_frame_at   TEXT,
  reconnect_count INTEGER NOT NULL,
  dropped_frames  INTEGER NOT NULL,
  updated_at      TEXT NOT NULL,
  details_json    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_current_camera_status_tank ON current_camera_status(tank_id);

CREATE TABLE IF NOT EXISTS camera_profiles (
  camera_id            TEXT PRIMARY KEY,
  tank_id              TEXT,
  display_name         TEXT NOT NULL,
  vendor               TEXT,
  host                 TEXT,
  rtsp_port            INTEGER,
  http_port            INTEGER,
  username             TEXT,
  password_secret_ref  TEXT,
  position             TEXT,
  purpose_json         TEXT NOT NULL,
  stream_profiles_json TEXT NOT NULL,
  clip_policy_json     TEXT NOT NULL,
  status               TEXT NOT NULL,
  metadata_json        TEXT NOT NULL,
  updated_at           TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_camera_profiles_tank ON camera_profiles(tank_id);

CREATE TABLE IF NOT EXISTS tank_profiles (
  tank_id          TEXT PRIMARY KEY,
  platform_tank_id TEXT,
  display_name     TEXT NOT NULL,
  species          TEXT NOT NULL,
  system_type      TEXT NOT NULL,
  volume_m3        REAL,
  biomass_kg       REAL,
  fish_count       INTEGER,
  avg_weight_g     REAL,
  target_ranges_json TEXT NOT NULL,
  metadata_json      TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tank_profiles_platform_tank ON tank_profiles(platform_tank_id);

CREATE TABLE IF NOT EXISTS open_alerts (
  alert_id         TEXT PRIMARY KEY,
  alert_dedupe_key TEXT NOT NULL UNIQUE,
  alert_type       TEXT NOT NULL,
  severity         TEXT NOT NULL,
  subject_kind     TEXT NOT NULL,
  subject_id       TEXT NOT NULL,
  rule_id          TEXT,
  status           TEXT NOT NULL,
  raised_at        TEXT NOT NULL,
  updated_at       TEXT NOT NULL,
  payload_json     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS control_commands (
  command_id       TEXT PRIMARY KEY,
  idempotency_key  TEXT NOT NULL UNIQUE,
  target_device_id TEXT NOT NULL,
  command_type     TEXT NOT NULL,
  status           TEXT NOT NULL,
  requested_at     TEXT NOT NULL,
  expires_at       TEXT NOT NULL,
  last_event_id    TEXT NOT NULL,
  payload_json     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_batches (
  batch_id        TEXT PRIMARY KEY,
  from_sequence   INTEGER NOT NULL,
  to_sequence     INTEGER NOT NULL,
  status          TEXT NOT NULL,
  attempt         INTEGER NOT NULL DEFAULT 0,
  next_retry_at   TEXT,
  created_at      TEXT NOT NULL,
  sent_at         TEXT,
  acknowledged_at TEXT,
  remote_ack_id   TEXT,
  error_json      TEXT
);

CREATE TABLE IF NOT EXISTS sync_batch_events (
  batch_id       TEXT NOT NULL,
  event_sequence INTEGER NOT NULL,
  event_id       TEXT NOT NULL,
  status         TEXT NOT NULL,
  error_code     TEXT,
  error_message  TEXT,
  PRIMARY KEY (batch_id, event_sequence)
);

CREATE TABLE IF NOT EXISTS runtime_kv (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS water_quality_buckets (
  tank_id             TEXT NOT NULL,
  bucket_start        TEXT NOT NULL,
  bucket_sec          INTEGER NOT NULL,
  temperature_c_avg   REAL,
  ph_avg              REAL,
  do_mg_l_avg         REAL,
  quality             TEXT NOT NULL,
  sample_count        INTEGER NOT NULL,
  suspect_count       INTEGER NOT NULL,
  source_reading_ids_json TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  PRIMARY KEY (tank_id, bucket_start)
);

CREATE INDEX IF NOT EXISTS idx_water_quality_buckets_tank_time ON water_quality_buckets(tank_id, bucket_start);

CREATE TABLE IF NOT EXISTS feeding_impact_analyses (
  feeding_id          TEXT PRIMARY KEY,
  analysis_id         TEXT NOT NULL UNIQUE,
  tank_id             TEXT NOT NULL,
  feed_amount_g       REAL NOT NULL,
  fed_at              TEXT NOT NULL,
  do_baseline_mg_l    REAL,
  do_min_post_mg_l    REAL,
  do_drop_mg_l        REAL,
  do_recovery_min     INTEGER,
  ph_delta            REAL,
  temp_delta_c        REAL,
  quality             TEXT NOT NULL,
  reason_codes_json   TEXT NOT NULL,
  updated_at          TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_feeding_impact_analyses_tank_time ON feeding_impact_analyses(tank_id, fed_at);
