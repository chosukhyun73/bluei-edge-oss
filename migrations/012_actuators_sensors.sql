-- Phase 1 multi-tank: split devices into actuators (control) and sensors (measurement).
-- Coexists with the existing devices/adapters model (current_device_status from 001).
-- Phase 2 will migrate the existing devices into actuators/sensors and deprecate the old shape.
-- References: docs/39-multi-tank-feeder-system-design.md §1.3, §9.1.

CREATE TABLE IF NOT EXISTS actuators (
  device_id         TEXT PRIMARY KEY,
  device_type       TEXT NOT NULL,         -- 'feeder' | 'heat_pump' | 'pump' | 'uv_sterilizer' | 'biofilter' | ...
  site_id           TEXT,
  tank_id           TEXT,
  wtg_id            TEXT,
  controller_id     TEXT,                  -- nullable (some actuators are software-driven)
  model             TEXT NOT NULL DEFAULT '',
  rated_power_w     REAL,
  position_json     TEXT NOT NULL DEFAULT '{}',  -- {angle_deg, radius_m, feeding_modifier}
  capabilities_json TEXT NOT NULL DEFAULT '[]',
  metadata_json     TEXT NOT NULL DEFAULT '{}',
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_actuators_tank ON actuators(tank_id);
CREATE INDEX IF NOT EXISTS idx_actuators_site ON actuators(site_id);
CREATE INDEX IF NOT EXISTS idx_actuators_wtg  ON actuators(wtg_id);
CREATE INDEX IF NOT EXISTS idx_actuators_type ON actuators(device_type);

CREATE TABLE IF NOT EXISTS sensors (
  sensor_id         TEXT PRIMARY KEY,
  sensor_type       TEXT NOT NULL,         -- 'water_quality' | 'water_quality_with_gps' | 'feed_weight' | ...
  site_id           TEXT,
  tank_id           TEXT,
  wtg_id            TEXT,
  position          TEXT NOT NULL DEFAULT '',    -- 'intake' | 'outlet' | 'in_tank' | 'north_entry' | ...
  hardware          TEXT NOT NULL DEFAULT '',    -- 'esp32' | 'raspberry_pi' | 'latte_panda' | ...
  capabilities_json TEXT NOT NULL DEFAULT '[]',
  metadata_json     TEXT NOT NULL DEFAULT '{}',
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sensors_tank ON sensors(tank_id);
CREATE INDEX IF NOT EXISTS idx_sensors_site ON sensors(site_id);
CREATE INDEX IF NOT EXISTS idx_sensors_wtg  ON sensors(wtg_id);
CREATE INDEX IF NOT EXISTS idx_sensors_type ON sensors(sensor_type);
