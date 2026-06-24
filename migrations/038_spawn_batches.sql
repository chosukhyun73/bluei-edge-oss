-- 산란 배치(spawn batch) = 알 lot — 종묘장 산란→부화 추적 단위.
-- 어미 계군(female/male cohort)에서 이어지며 족보(origin_type/region/supplier/generation)를
-- 스냅샷 보존한다(GDST sourceOfBroodstock). lot_code 는 추후 종자/판매 QR 의 주체.
-- 산란과 부화는 같은 레코드(부화 결과는 갱신). status: incubating|hatched|discarded|sold.
CREATE TABLE IF NOT EXISTS spawn_batches (
  batch_id           TEXT PRIMARY KEY,
  group_id           TEXT NOT NULL,
  tank_id            TEXT,                  -- 산란조/부화조
  species            TEXT NOT NULL,
  lot_code           TEXT,                  -- 알 lot 코드(추적/QR 주체)
  female_cohort_id   TEXT,                  -- 모계 어미 계군
  male_cohort_id     TEXT,                  -- 부계 어미 계군
  origin_type        TEXT,                  -- 족보 스냅샷(wild|domestic)
  origin_region      TEXT,
  supplier           TEXT,
  generation         TEXT,
  spawn_date         TEXT,
  egg_count          INTEGER,
  egg_volume_ml      REAL,
  fertilization_rate REAL,                  -- 수정률 %
  hatch_date         TEXT,
  hatched_count      INTEGER,
  hatch_rate         REAL,                  -- 부화율 %
  status             TEXT NOT NULL DEFAULT 'incubating',
  buyer              TEXT,                  -- 직판 시 구매처(로컬)
  notes              TEXT,
  metadata_json      TEXT,
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_spawn_group ON spawn_batches(group_id);
