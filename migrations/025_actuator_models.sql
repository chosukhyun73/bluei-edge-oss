-- 025_actuator_models.sql
-- C-13b 액추에이터 모델 라이브러리 + 인스턴스에 모델/안전/운영 메타 추가.
-- actuator_models: vendor/product/카테고리/제어방식 등 액추에이터 모델별 특성 라이브러리.
-- actuators ADD COLUMN: model_id (모델 FK, nullable), mount_location (어디 설치),
--   safety_role_json (운영 안전 의도 multi-select), operating_mode (auto/manual/standby/maintenance/fault),
--   alarm_thresholds_json (선택), last_maintenance_at / next_maintenance_due_at.
-- schema_migrations ledger (storage.Migrate) 가 추적 — ADD COLUMN 한 번만 실행.

CREATE TABLE IF NOT EXISTS actuator_models (
  model_id TEXT PRIMARY KEY,
  vendor TEXT NOT NULL,
  product_code TEXT NOT NULL,
  display_name TEXT NOT NULL,
  device_category TEXT NOT NULL CHECK (device_category IN (
    'pump','aerator','oxygen_cone','heater','chiller','uv_sterilizer',
    'led_light','feeder','valve','biofilter','drum_filter','dosing_pump',
    'ozonator','blower','skimmer','other'
  )),
  rated_power_w REAL,
  capacity_value REAL,
  capacity_unit TEXT,
  control_method TEXT CHECK (control_method IN (
    'on_off','pwm','4-20ma','0-10v','modbus','mqtt','esp32_controller','manual','other'
  )),
  response_time_s REAL,
  control_range_min REAL,
  control_range_max REAL,
  control_range_unit TEXT,
  consumable_replacement_days INTEGER,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

ALTER TABLE actuators ADD COLUMN model_id TEXT;
ALTER TABLE actuators ADD COLUMN mount_location TEXT;
ALTER TABLE actuators ADD COLUMN safety_role_json TEXT;
ALTER TABLE actuators ADD COLUMN operating_mode TEXT DEFAULT 'auto';
ALTER TABLE actuators ADD COLUMN alarm_thresholds_json TEXT;
ALTER TABLE actuators ADD COLUMN last_maintenance_at TEXT;
ALTER TABLE actuators ADD COLUMN next_maintenance_due_at TEXT;
