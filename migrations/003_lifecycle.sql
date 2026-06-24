-- D-1: 입식/출하 lifecycle projection.
-- Cage/Tank당 활성 stocking lineage 1행. 운영자가 출하하면 status=harvested 로 변경.
-- 다음 입식 시 새 row 로 덮음 (이전 row 는 events 테이블에 영구 보존되므로 history 손실 없음).

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
  status                  TEXT NOT NULL,  -- active | harvested
  updated_at              TEXT NOT NULL
);
