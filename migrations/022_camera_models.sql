-- 022_camera_models.sql
-- 카메라 모델 라이브러리 + 인스턴스에 모델/설치 정보 추가.
-- camera_models: vendor/product/lens_type 등 카메라 모델별 특성 라이브러리.
--   single vs dual 구분 — dual 일 때만 baseline_mm / stereo_calibration_json 의미.
-- camera_profiles ADD COLUMN model_id : 인스턴스 ↔ 모델 FK (nullable).
-- mounting_height_m / underwater_depth_m : 인스턴스 고유 설치 정보.
-- schema_migrations ledger (storage.Migrate) 가 추적 — ADD COLUMN 한 번만 실행.

CREATE TABLE IF NOT EXISTS camera_models (
  model_id TEXT PRIMARY KEY,
  vendor TEXT NOT NULL,
  product_code TEXT NOT NULL,
  display_name TEXT NOT NULL,
  lens_type TEXT NOT NULL CHECK (lens_type IN ('single','dual','fisheye','ptz','other')),
  baseline_mm REAL,
  stereo_calibration_json TEXT,
  resolution_w INTEGER,
  resolution_h INTEGER,
  fov_deg REAL,
  fps INTEGER,
  night_mode INTEGER NOT NULL DEFAULT 0,
  protocols_json TEXT,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

ALTER TABLE camera_profiles ADD COLUMN model_id TEXT;
ALTER TABLE camera_profiles ADD COLUMN mounting_height_m REAL;
ALTER TABLE camera_profiles ADD COLUMN underwater_depth_m REAL;
