-- 021_tank_physical_dimensions.sql
-- Tank 물리 정보 (형태/직경/가로/세로/수심) 추가 — C-9 Tank 도메인 운영.
-- 모두 nullable — 기존 row 영향 없음. form_factor 가 정의되어야 직경/가로 등 의미.
-- schema_migrations ledger (storage.Migrate) 가 추적하므로 ADD COLUMN 한 번만 실행됨.

ALTER TABLE tank_profiles ADD COLUMN form_factor TEXT;
ALTER TABLE tank_profiles ADD COLUMN diameter_m REAL;
ALTER TABLE tank_profiles ADD COLUMN length_m REAL;
ALTER TABLE tank_profiles ADD COLUMN width_m REAL;
ALTER TABLE tank_profiles ADD COLUMN depth_m REAL;
