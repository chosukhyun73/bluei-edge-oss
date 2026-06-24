-- 입식/출하 = 사업자(site) 단위 거래. 수조는 분배(입식)·line item(출하)의 결과.
-- 입식 배치: 공급처에서 한 배치 받아 여러 수조에 분배(분리입식). 출하: 한 거래처로 여러 수조/부분 출하.
-- 분배/line item 마다 기존 수조 lifecycle(tank.stocking/harvest.recorded)이 파생 생성된다.
-- allocations_json / lines_json 에 수조별 내역(JSON 배열) 보관.
-- References: docs/49-gdst-traceability-contract.md.

CREATE TABLE IF NOT EXISTS site_stockings (
  site_stocking_id   TEXT PRIMARY KEY,
  site_id            TEXT NOT NULL,
  supplier_id        TEXT,            -- partner_id (부화장)
  supplier_name      TEXT,
  species            TEXT NOT NULL,
  growth_stage       TEXT NOT NULL,
  source_hatchery    TEXT,
  batch_lot_no       TEXT,
  total_count        INTEGER NOT NULL DEFAULT 0,
  total_avg_weight_g REAL,
  total_biomass_kg   REAL,
  allocations_json   TEXT NOT NULL DEFAULT '[]',  -- [{tank_id,count,avg_weight_g,lot_no,stocking_id}]
  stocked_at         TEXT NOT NULL,
  operator_id        TEXT NOT NULL DEFAULT '',
  notes              TEXT,
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS site_harvests (
  site_harvest_id    TEXT PRIMARY KEY,
  site_id            TEXT NOT NULL,
  buyer_id           TEXT,            -- partner_id (구매처)
  buyer_name         TEXT,
  total_count        INTEGER NOT NULL DEFAULT 0,
  total_biomass_kg   REAL,
  lines_json         TEXT NOT NULL DEFAULT '[]',  -- [{tank_id,lot_no,count,avg_weight_g,full_close,harvest_id}]
  vehicle_info       TEXT,
  harvested_at       TEXT NOT NULL,
  operator_id        TEXT NOT NULL DEFAULT '',
  notes              TEXT,
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_site_stockings_site ON site_stockings(site_id);
CREATE INDEX IF NOT EXISTS idx_site_harvests_site  ON site_harvests(site_id);

-- 거래처를 사업자(site) 소속으로.
ALTER TABLE partners ADD COLUMN site_id TEXT;
CREATE INDEX IF NOT EXISTS idx_partners_site ON partners(site_id);
