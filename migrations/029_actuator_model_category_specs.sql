-- 029_actuator_model_category_specs.sql
-- C-13b 카테고리별 spec 확장.
-- 1) actuator_models 에 category_specs TEXT (카테고리별 spec JSON 문자열) 추가.
-- 2) device_category CHECK 에 'circulation_pump','heat_pump','air_pump' 3종 추가 (기존 16종 유지).
-- SQLite 는 CHECK 변경에 테이블 재생성이 필요 → 표준 재생성 패턴 사용.
-- 기존 공통 컬럼(rated_power_w/capacity_value 등) 모두 유지 — 하위호환.

CREATE TABLE IF NOT EXISTS actuator_models_new (
  model_id TEXT PRIMARY KEY,
  vendor TEXT NOT NULL,
  product_code TEXT NOT NULL,
  display_name TEXT NOT NULL,
  device_category TEXT NOT NULL CHECK (device_category IN (
    'pump','aerator','oxygen_cone','heater','chiller','uv_sterilizer',
    'led_light','feeder','valve','biofilter','drum_filter','dosing_pump',
    'ozonator','blower','skimmer','other',
    'circulation_pump','heat_pump','air_pump'
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
  category_specs TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

INSERT INTO actuator_models_new (
  model_id, vendor, product_code, display_name, device_category,
  rated_power_w, capacity_value, capacity_unit, control_method, response_time_s,
  control_range_min, control_range_max, control_range_unit,
  consumable_replacement_days, notes, category_specs, created_at, updated_at
)
SELECT
  model_id, vendor, product_code, display_name, device_category,
  rated_power_w, capacity_value, capacity_unit, control_method, response_time_s,
  control_range_min, control_range_max, control_range_unit,
  consumable_replacement_days, notes, NULL, created_at, updated_at
FROM actuator_models;

DROP TABLE actuator_models;
ALTER TABLE actuator_models_new RENAME TO actuator_models;
