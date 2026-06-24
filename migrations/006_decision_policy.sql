-- C-4: Cage/Tank별 pending_notify 자동 실행 정책 projection.
-- auto_execute_enabled 기본값 0 (false) — 운영자가 명시적으로 켜야만 타이머 작동.
-- grace_minutes: 알림 후 몇 분 뒤 자동 실행할지. 시스템 기본값은 Config 에서 fallback.

CREATE TABLE IF NOT EXISTS current_tank_decision_policy (
  tank_id              TEXT PRIMARY KEY,
  auto_execute_enabled INTEGER NOT NULL DEFAULT 0,  -- 0/1 boolean
  grace_minutes        INTEGER NOT NULL DEFAULT 10,
  updated_at           TEXT NOT NULL,
  updated_by           TEXT NOT NULL
);
