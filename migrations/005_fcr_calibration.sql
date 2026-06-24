-- D-4: Cage/Tank별 FCR 자동 보정 projection.
-- sampling 시점에 추정 vs 실측 차이로 Cage/Tank별 expected_FCR 보정.

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
