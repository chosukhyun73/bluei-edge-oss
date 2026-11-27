-- 가공공장 구성: 공장 정보, 구역, 설비, 공정 센서, 로컬 계정.
-- 공정 센서는 수조에 맞춰진 기존 sensors 테이블과 따로 둔다(양식 쪽 코드를 건드리지 않는다).
-- References: docs/54-haccp-process-flow-ccp-design.md §17.2, §19.4, §21.

CREATE TABLE IF NOT EXISTS plant_profiles (
  site_id              TEXT PRIMARY KEY,          -- sites.site_type = 'processing'
  business_no          TEXT NOT NULL DEFAULT '',  -- 사업자번호
  license_name         TEXT NOT NULL DEFAULT '',  -- 영업 허가·등록·신고 명칭 (GDST processorLicense 의 standard)
  license_no           TEXT NOT NULL DEFAULT '',  -- 허가·등록·신고 번호
  license_authority    TEXT NOT NULL DEFAULT '',  -- 발급 당국
  haccp_cert_no        TEXT NOT NULL DEFAULT '',  -- HACCP 인증번호
  human_welfare_policy TEXT NOT NULL DEFAULT '',  -- '' | none | internal_policy | third_party_audit
  retention_years      INTEGER NOT NULL DEFAULT 3,
  created_at           TEXT NOT NULL,
  updated_at           TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS plant_zones (
  zone_id       TEXT PRIMARY KEY,
  site_id       TEXT NOT NULL,
  name          TEXT NOT NULL,
  hygiene_class TEXT NOT NULL DEFAULT 'general',  -- general | clean
  temp_class    TEXT NOT NULL DEFAULT 'ambient',  -- ambient | chilled | frozen
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plant_zones_site ON plant_zones(site_id);

CREATE TABLE IF NOT EXISTS plant_equipment (
  equipment_id TEXT PRIMARY KEY,
  site_id      TEXT NOT NULL,
  zone_id      TEXT NOT NULL DEFAULT '',
  kind         TEXT NOT NULL,   -- cold_room | freezer | cooker | washer | metal_detector | scale | tag_reader | other
  name         TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plant_equipment_site ON plant_equipment(site_id);

CREATE TABLE IF NOT EXISTS plant_sensors (
  sensor_id          TEXT PRIMARY KEY,
  site_id            TEXT NOT NULL,
  zone_id            TEXT NOT NULL DEFAULT '',
  equipment_id       TEXT NOT NULL DEFAULT '',
  metric             TEXT NOT NULL,              -- temperature | core_temperature | flow | weight | count | zone_presence | ...
  unit               TEXT NOT NULL DEFAULT '',
  is_mock            INTEGER NOT NULL DEFAULT 0, -- 모의 센서
  calibration_due_at TEXT,
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plant_sensors_site ON plant_sensors(site_id);

-- 현장 서명용 로컬 계정. 인터넷 없이 PIN 으로 본인 확인한다.
CREATE TABLE IF NOT EXISTS local_users (
  user_id    TEXT PRIMARY KEY,
  site_id    TEXT NOT NULL,
  name       TEXT NOT NULL,
  role       TEXT NOT NULL,     -- admin | reviewer | monitor
  pin_salt   TEXT NOT NULL,
  pin_hash   TEXT NOT NULL,
  active     INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_local_users_site ON local_users(site_id);
