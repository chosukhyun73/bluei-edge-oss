-- D-5: 일일 추정 체중 스냅샷 (Cage/Tank 시계열).
-- 같은 날에 worker가 여러 번 tick 하면 마지막 값으로 UPSERT.
-- 운영자 chart 가 30일/90일 기간 조회.

CREATE TABLE IF NOT EXISTS tank_weight_history (
  tank_id                TEXT NOT NULL,
  snapshot_date          TEXT NOT NULL,     -- YYYY-MM-DD (Asia/Seoul wall clock)
  estimated_avg_weight_g REAL NOT NULL,
  anchor_weight_g        REAL NOT NULL,
  anchor_source          TEXT NOT NULL,     -- "stocking" | "sampling"
  days_since_anchor      INTEGER NOT NULL,
  expected_fcr           REAL NOT NULL,
  fcr_source             TEXT NOT NULL,     -- "default" | "calibrated"
  cumulative_feed_g      REAL NOT NULL,
  quality                TEXT NOT NULL,     -- "ok" | "stale_sampling" | "low_data"
  snapshot_at            TEXT NOT NULL,     -- exact tick timestamp (RFC3339)
  PRIMARY KEY (tank_id, snapshot_date)
);

CREATE INDEX IF NOT EXISTS idx_weight_history_tank_date
  ON tank_weight_history (tank_id, snapshot_date DESC);
