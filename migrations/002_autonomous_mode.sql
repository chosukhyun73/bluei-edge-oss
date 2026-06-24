-- Phase 4 C-1: 자율 운영 모드 projection.
-- Cage/Tank별 현재 모드 1행. audit 이력은 events 테이블의 tank.autonomous_mode.changed.

CREATE TABLE IF NOT EXISTS current_tank_autonomous_mode (
  tank_id     TEXT PRIMARY KEY,
  mode        TEXT NOT NULL,
  reason      TEXT,
  changed_at  TEXT NOT NULL,
  changed_by  TEXT NOT NULL
);
