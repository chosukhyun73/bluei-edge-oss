-- 017_arbiter_decisions.sql
-- Arbiter audit log + operator intent memos (Phase 5)

CREATE TABLE IF NOT EXISTS arbiter_decisions (
  decision_id       TEXT PRIMARY KEY,
  tank_id           TEXT NOT NULL,
  source            TEXT NOT NULL,
  priority          TEXT NOT NULL,
  accepted          INTEGER NOT NULL,   -- 1=accepted, 0=rejected
  rejection_reason  TEXT,
  existing_cycle_id TEXT,               -- 충돌 사이클 ID (거부 시)
  resulting_cycle_id TEXT,              -- 생성된 사이클 ID (허용 시)
  intent_id         TEXT,
  submitted_at      TEXT NOT NULL,
  decided_at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_arbiter_decisions_tank
  ON arbiter_decisions(tank_id, decided_at DESC);

CREATE TABLE IF NOT EXISTS operator_intents (
  intent_id           TEXT PRIMARY KEY,
  operator_id         TEXT NOT NULL DEFAULT '',
  tank_id             TEXT,
  related_cycle_id    TEXT,
  related_decision_id TEXT,
  intent_type         TEXT NOT NULL,  -- 'feed_now' | 'skip_cycle' | 'change_pattern' | 'general_note'
  reason              TEXT NOT NULL,
  context_json        TEXT NOT NULL DEFAULT '{}',
  recorded_at         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_operator_intents_tank
  ON operator_intents(tank_id, recorded_at DESC);
