-- 어미 계군(broodstock cohort) — 종묘장 어미 족보(출신성분) 관리.
-- 같은 출신의 어미 한 묶음. broodstock 단계 그룹(group.metadata.stage_role='broodstock')에 속한다.
-- 산란→종자 출하 시 origin_type/region/supplier/generation 이 GDST sourceOfBroodstock 로 전파된다.
CREATE TABLE IF NOT EXISTS broodstock_cohorts (
  cohort_id        TEXT PRIMARY KEY,
  group_id         TEXT NOT NULL,
  tank_id          TEXT,                       -- 어미조(선택)
  species          TEXT NOT NULL,
  origin_type      TEXT NOT NULL,              -- 'wild'(자연산) | 'domestic'(사육산) — GDST sourceOfBroodstock
  origin_region    TEXT,                       -- 해역·지역
  supplier         TEXT,                       -- 공급자/입수처
  generation       TEXT,                       -- F0/F1/F2...
  parent_cohort_id TEXT,                       -- 상위 계군(세대 체인)
  acquired_date    TEXT,                       -- YYYY-MM-DD
  male_count       INTEGER NOT NULL DEFAULT 0,
  female_count     INTEGER NOT NULL DEFAULT 0,
  maturity         TEXT,                       -- immature|maturing|mature|spent
  notes            TEXT,
  metadata_json    TEXT,
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_broodstock_group ON broodstock_cohorts(group_id);
