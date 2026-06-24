-- 자어 육성(larval rearing) + 먹이배양실(live feed culture) — 종묘장 자어 단계.
-- larval_batches: 부화 후 자어조 사육. source_lot_code(부모 알 lot=spawn_batches.lot_code)로
--   족보(origin_*)를 이어받아 GDST 연속성 유지. 생존율/발달단계 추적.
-- live_feed_cultures: 로티퍼·알테미아 등 배양(자어 급이용). 연어류(난황 자가급이)는 불필요.
CREATE TABLE IF NOT EXISTS larval_batches (
  batch_id        TEXT PRIMARY KEY,
  group_id        TEXT NOT NULL,
  tank_id         TEXT,                       -- 자어조
  species         TEXT NOT NULL,
  source_lot_code TEXT,                       -- 부모 알 lot(spawn_batches.lot_code)
  origin_type     TEXT,                       -- 족보 스냅샷(wild|domestic)
  origin_region   TEXT,
  supplier        TEXT,
  generation      TEXT,
  start_date      TEXT,
  initial_count   INTEGER,
  current_count   INTEGER,
  survival_rate   REAL,                       -- % (자동: current/initial)
  dev_stage       TEXT,                       -- yolk_sac|first_feeding|metamorphosis|juvenile
  density_per_l   REAL,
  status          TEXT NOT NULL DEFAULT 'rearing',  -- rearing|graduated|discarded
  notes           TEXT,
  metadata_json   TEXT,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_larval_group ON larval_batches(group_id);

CREATE TABLE IF NOT EXISTS live_feed_cultures (
  culture_id        TEXT PRIMARY KEY,
  group_id          TEXT NOT NULL,
  tank_id           TEXT,                     -- 배양조(선택)
  feed_type         TEXT NOT NULL,            -- rotifer|artemia|microalgae|copepod|other
  strain            TEXT,
  start_date        TEXT,
  volume_l          REAL,
  density_per_ml    REAL,
  last_harvest_date TEXT,
  harvest_amount    TEXT,
  status            TEXT NOT NULL DEFAULT 'culturing',  -- culturing|harvesting|crashed|ended
  notes             TEXT,
  metadata_json     TEXT,
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_livefeed_group ON live_feed_cultures(group_id);
