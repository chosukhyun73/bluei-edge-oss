-- 018_arbiter_preemption.sql
-- feed_cycles에 priority 컬럼 추가, arbiter_decisions에 preempted_cycle_id 추가 (Phase 6)

ALTER TABLE feed_cycles ADD COLUMN priority TEXT NOT NULL DEFAULT 'manual_override';
ALTER TABLE arbiter_decisions ADD COLUMN preempted_cycle_id TEXT;

CREATE INDEX IF NOT EXISTS idx_feed_cycles_priority ON feed_cycles(priority);
