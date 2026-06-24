-- 023_camera_view_geometry.sql
-- C-12 — 카메라 인스턴스 메타 정정.
-- 기존 `position` 컬럼은 "위치(어디 위)" 와 "시점/구도(어떻게 보는가)" 의미가 섞여 있어
-- AI 알고리즘 분기 불가. 두 차원을 분리:
--   mount_location : 어디 위에 설치되었는지 (feeder_zone/water_intake/...)
--   view_angle     : 카메라가 어떤 구도로 보는가 (top_down/oblique_top/side_horizontal/underwater_top/underwater_side)
--                    → AI 알고리즘 선택의 핵심 (탑뷰=마릿수, 측면=행동, 수중 듀얼+underwater_side=크기측정)
-- 또한 `mounting_height_m` 의 기준이 모호 (수면? 바닥? 지면?) — 수면 기준으로 명시:
--   height_from_water_m : 양수=수면 위, 음수=수중. 운영자가 의미 모르고 입력하는 문제 해결.
--   tilt_deg            : 0=수평, 90=수직 아래. (옵션)
--
-- 기존 컬럼 (`position`, `mounting_height_m`, `underwater_depth_m`) 은 보존 — backward compat.
-- 신규 등록은 새 컬럼 사용. 기존 row 는 best-effort 재해석으로 채움.
-- schema_migrations ledger (storage.Migrate) 가 추적 — ADD COLUMN 한 번만 실행.

ALTER TABLE camera_profiles ADD COLUMN mount_location TEXT;
ALTER TABLE camera_profiles ADD COLUMN view_angle TEXT;
ALTER TABLE camera_profiles ADD COLUMN height_from_water_m REAL;
ALTER TABLE camera_profiles ADD COLUMN tilt_deg REAL;

-- best-effort 재해석 (`position` enum → view_angle / mount_location)
UPDATE camera_profiles SET view_angle = 'top_down'
  WHERE view_angle IS NULL AND position = 'overhead';
UPDATE camera_profiles SET view_angle = 'side_horizontal'
  WHERE view_angle IS NULL AND position = 'side';
UPDATE camera_profiles SET view_angle = 'underwater_side'
  WHERE view_angle IS NULL AND position = 'underwater';

UPDATE camera_profiles SET mount_location = 'feeder_zone'
  WHERE mount_location IS NULL AND position = 'feeding_zone';
UPDATE camera_profiles SET mount_location = 'water_intake'
  WHERE mount_location IS NULL AND position = 'water_intake';
UPDATE camera_profiles SET mount_location = 'water_outlet'
  WHERE mount_location IS NULL AND position = 'outlet';

-- 기존 mounting_height_m 는 best-effort 로 수면 기준으로 복사.
-- (의미 모호하던 값이지만 보존이 데이터 손실보다 안전)
UPDATE camera_profiles SET height_from_water_m = mounting_height_m
  WHERE height_from_water_m IS NULL AND mounting_height_m IS NOT NULL;
