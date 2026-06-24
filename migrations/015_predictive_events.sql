-- Phase 1 multi-tank: predictive water-quality projection store + cycle/pulse audit shells.
-- Phase 4 (D-6 / D-7 / D-8 + C-3p) will populate these; schema scaffold only.
-- Raw events still live in `events` (001); these are projections optimized for query.
-- References: docs/39-multi-tank-feeder-system-design.md §4, §7.

-- D-8 short-horizon water-quality forecast snapshot per WTG.
CREATE TABLE IF NOT EXISTS predictive_quality_forecasts (
  wtg_id              TEXT NOT NULL,
  forecast_at         TEXT NOT NULL,           -- when the forecast was generated
  horizon_min         INTEGER NOT NULL,        -- typically 30 or 60
  predicted_metrics_json TEXT NOT NULL,        -- {nh3, do, ph, ...}
  inputs_json         TEXT NOT NULL DEFAULT '{}',  -- {waste_load, capacity, env}
  model_version       TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (wtg_id, forecast_at)
);

CREATE INDEX IF NOT EXISTS idx_pq_forecasts_recent
  ON predictive_quality_forecasts (wtg_id, forecast_at DESC);

-- C-3p predictive blocks — audit trail of cycles blocked by predicted threshold breach.
CREATE TABLE IF NOT EXISTS predictive_blocks (
  block_id          TEXT PRIMARY KEY,
  wtg_id            TEXT NOT NULL,
  tank_id           TEXT,
  cycle_id          TEXT,
  reason            TEXT NOT NULL,             -- 'nh3_threshold' | 'do_threshold' | 'ph_drift' | ...
  predicted_value   REAL,
  threshold_value   REAL,
  forecast_at       TEXT NOT NULL,
  blocked_at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_predictive_blocks_wtg
  ON predictive_blocks (wtg_id, blocked_at DESC);

-- Feed cycle audit (state machine: idle → pulse → gap_observation → complete).
CREATE TABLE IF NOT EXISTS feed_cycles (
  cycle_id            TEXT PRIMARY KEY,
  tank_id             TEXT NOT NULL,
  controller_id       TEXT,
  mode                TEXT NOT NULL,            -- 'adaptive' | 'fixed' | 'legacy_dispense'
  target_amount_g     REAL,
  pulses_executed     INTEGER NOT NULL DEFAULT 0,
  total_amount_g      REAL NOT NULL DEFAULT 0,
  avg_gap_ms          REAL,
  termination_reason  TEXT,                     -- 'satiation' | 'unconsumed' | 'predictive_block' | 'safety_block' | 'operator_stop' | 'completed'
  started_at          TEXT NOT NULL,
  completed_at        TEXT,
  vision_summary_json TEXT NOT NULL DEFAULT '{}',
  quality_impact_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_feed_cycles_tank ON feed_cycles (tank_id, started_at DESC);
