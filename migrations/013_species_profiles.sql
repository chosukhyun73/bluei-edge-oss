-- Phase 1 multi-tank: species profiles + per-stage feeding defaults + waste model.
-- Source: app.bluei.kr canonical defaults → operator override → AI calibration (8-D hybrid).
-- Used by feed_cycle state machine and D-6 Feed-to-Waste Estimator.
-- References: docs/39-multi-tank-feeder-system-design.md §1.3, §4.1.

CREATE TABLE IF NOT EXISTS species_profiles (
  species              TEXT PRIMARY KEY,        -- 'atlantic_salmon' | 'red_seabream' | ...
  display_name         TEXT NOT NULL DEFAULT '',
  lifecycle_stages_json TEXT NOT NULL DEFAULT '{}',  -- {fry: {...}, fingerling: {...}, growout: {...}}
  waste_model_json     TEXT NOT NULL DEFAULT '{}',
  source               TEXT NOT NULL DEFAULT 'default',  -- 'default' | 'override' | 'calibrated'
  created_at           TEXT NOT NULL,
  updated_at           TEXT NOT NULL
);
