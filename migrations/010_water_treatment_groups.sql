-- Phase 1 multi-tank: Water Treatment Groups (WTG) for land RAS.
-- A WTG ties together multiple tanks that share heat pump / UV / biofilter / circulation capacity.
-- Inputs to D-7 (capacity model) and C-3p (predictive water-quality gate).
-- References: docs/39-multi-tank-feeder-system-design.md §1.3, §4.2.

CREATE TABLE IF NOT EXISTS water_treatment_groups (
  wtg_id            TEXT PRIMARY KEY,
  site_id           TEXT NOT NULL,
  name              TEXT NOT NULL DEFAULT '',
  shared_equipment_json TEXT NOT NULL DEFAULT '{}',  -- {heat_pump, uv_sterilizer, ...}
  intake_sensor_id  TEXT,
  outlet_sensor_id  TEXT,
  volume_m3         REAL,
  nh3_processing_kg_per_h REAL,
  flow_rate_m3_per_h REAL,
  feeding_policy_json TEXT NOT NULL DEFAULT '{}',
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  FOREIGN KEY (site_id) REFERENCES sites(site_id)
);

CREATE INDEX IF NOT EXISTS idx_wtg_site ON water_treatment_groups(site_id);
