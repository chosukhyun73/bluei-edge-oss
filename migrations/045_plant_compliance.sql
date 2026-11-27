-- 인증서 원장과 사업장이 고른 적용 제도.
-- 제도별 요구 항목(대응표)은 코드에 실린 파일(internal/compliance/profiles)이고, 여기에는 선택만 둔다.
-- References: docs/54-haccp-process-flow-ccp-design.md §18.2, §18.4, §23.

CREATE TABLE IF NOT EXISTS plant_certificates (
  cert_id     TEXT PRIMARY KEY,
  site_id     TEXT NOT NULL,
  scheme      TEXT NOT NULL,               -- haccp_kr | haccp_kr_export | msc_coc | asc_coc | bap | brcgs | ifs | sqf | fssc22000 | iso22000 | other
  cert_no     TEXT NOT NULL,
  scope       TEXT NOT NULL DEFAULT '',    -- 인증 범위(품목·공정)
  holder_kind TEXT NOT NULL DEFAULT 'own', -- own(자사) | supplier(공급자)
  holder_name TEXT NOT NULL DEFAULT '',    -- 공급자일 때 상호
  valid_from  TEXT NOT NULL DEFAULT '',    -- YYYY-MM-DD
  valid_until TEXT NOT NULL DEFAULT '',    -- YYYY-MM-DD, '' = 만료일 없음
  verified_at TEXT NOT NULL DEFAULT '',    -- 유효성을 확인한 날 YYYY-MM-DD
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plant_certificates_site ON plant_certificates(site_id);

CREATE TABLE IF NOT EXISTS plant_compliance_profiles (
  site_id    TEXT NOT NULL,
  profile_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (site_id, profile_id)
);
