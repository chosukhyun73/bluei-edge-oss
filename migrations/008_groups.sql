-- Phase 1: Group (양식동/순환시스템) 도입.
-- Cage/Tank 들이 하나의 시스템 단위 (예: A동 순환, B동 해상) 로 묶임.
-- Group 정보는 YAML 로 1차 로드되어 startup 시 upsert.

CREATE TABLE IF NOT EXISTS group_profiles (
  group_id      TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  description   TEXT NOT NULL DEFAULT '',
  color         TEXT NOT NULL DEFAULT '',  -- HEX (e.g., #22c55e) — UI tint hint
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

-- tank_profiles 에 group_id FK (nullable — 기존 단일 Cage/Tank 호환).
ALTER TABLE tank_profiles ADD COLUMN group_id TEXT;
CREATE INDEX IF NOT EXISTS idx_tank_profiles_group ON tank_profiles(group_id);
