-- 024_sensor_models.sql
-- C-13a — 센서 모델 라이브러리 + 인스턴스 메타 정정.
-- 카메라 (C-11/C-12) 의 4 가지 패턴을 센서에 동일 적용:
--   1) 모델 라이브러리 별도 테이블 (sensor_models) — 동일 모델 여러 인스턴스 reuse.
--   2) 위치(어디) ≠ 의도(어떻게 쓸지) 분리.
--      mount_location  : 어디 위에 설치 (water_intake/water_outlet/tank_top/...)
--      measurement_role: 운영자 의도 (safety_gate_c3/feeding_decision/...)
--   3) 설치 기하 기준 통일 — installed_depth_m (수면 기준, 양수=위, 음수=아래).
--   4) AI/운영자 의도 multi-select — measurement_role_json (다중 선택).
--
-- 기존 컬럼 (position) 은 보존 — backward compat. 신규 등록은 새 컬럼 사용.
-- 기존 row 는 best-effort 재해석.
-- schema_migrations ledger (storage.Migrate) 가 추적 — ADD COLUMN 한 번만 실행.

CREATE TABLE IF NOT EXISTS sensor_models (
  model_id TEXT PRIMARY KEY,
  vendor TEXT NOT NULL,
  product_code TEXT NOT NULL,
  display_name TEXT NOT NULL,
  measurement_type TEXT NOT NULL CHECK (measurement_type IN (
    'water_temperature','ph','dissolved_oxygen','unionized_ammonia','nitrate',
    'nitrite','carbon_dioxide','total_suspended_solids','turbidity','salinity',
    'flow_rate','pump_pressure','water_level','light_intensity','feed_weight',
    'oxygen_saturation','redox','conductivity','multi','other'
  )),
  unit TEXT NOT NULL,
  range_min REAL,
  range_max REAL,
  accuracy_value REAL,
  accuracy_unit TEXT,
  response_time_s REAL,
  protocol TEXT CHECK (protocol IN ('modbus','rs485','rs232','4-20ma','0-10v','i2c','sdi-12','http','mqtt','other')),
  calibration_interval_days INTEGER,
  wet_dry TEXT CHECK (wet_dry IN ('wet_probe','inline','dry_mount','other')),
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

ALTER TABLE sensors ADD COLUMN model_id TEXT;
ALTER TABLE sensors ADD COLUMN mount_location TEXT;
ALTER TABLE sensors ADD COLUMN installed_depth_m REAL;
ALTER TABLE sensors ADD COLUMN measurement_role_json TEXT;
ALTER TABLE sensors ADD COLUMN calibration_last_at TEXT;
ALTER TABLE sensors ADD COLUMN calibration_due_at TEXT;

-- best-effort 재해석 (기존 position 문자열 → mount_location).
UPDATE sensors SET mount_location = 'water_intake'
  WHERE mount_location IS NULL AND position = 'intake';
UPDATE sensors SET mount_location = 'water_outlet'
  WHERE mount_location IS NULL AND position = 'outlet';
UPDATE sensors SET mount_location = 'mid_depth'
  WHERE mount_location IS NULL AND position = 'mid-depth';
