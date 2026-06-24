-- D-2: Cage/Tank sampling projection.
-- Cage/Tank당 마지막 sampling 1행. 영구 history 는 events 테이블 (tank.sampling.recorded).

CREATE TABLE IF NOT EXISTS current_tank_sampling (
  tank_id              TEXT PRIMARY KEY,
  latest_sampling_id   TEXT NOT NULL,
  stocking_id          TEXT,         -- 어떤 lineage 의 sampling 인지 (있으면)
  sampled_count        INTEGER NOT NULL,
  avg_weight_g         REAL NOT NULL,
  std_weight_g         REAL,
  min_weight_g         REAL,
  max_weight_g         REAL,
  health_score         INTEGER,      -- 0~10
  health_notes         TEXT,
  abnormal_count       INTEGER,
  sampled_at           TEXT NOT NULL,
  recorded_by          TEXT NOT NULL,
  updated_at           TEXT NOT NULL
);
