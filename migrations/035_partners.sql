-- 거래처(공급처/구매처) 마스터 — 부화장/사료공급사/약품공급사/구매처 등.
-- 입식 공급처, 사료·약품 공급사, 출하 거래처를 식별하고 생산자면허 등 서류를 연결한다(GDST party 식별).
-- 서류는 tank_documents(subject_type='partner', subject_id=partner_id) 로 첨부.
-- References: docs/49-gdst-traceability-contract.md.

CREATE TABLE IF NOT EXISTS partners (
  partner_id   TEXT PRIMARY KEY,
  partner_type TEXT NOT NULL,   -- hatchery | feed_supplier | drug_supplier | buyer | other
  name         TEXT NOT NULL,
  business_no  TEXT,            -- 사업자번호
  license_no   TEXT,            -- 생산자/양식 면허번호
  contact      TEXT,
  address      TEXT,
  gln          TEXT,            -- GS1 GLN (있으면)
  notes        TEXT,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_partners_type ON partners(partner_type);
