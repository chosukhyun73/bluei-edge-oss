-- 019_feed_cycle_intent.sql
-- C-1: feed_cycles 에 operator_intents 와의 영구 링크 추가.
-- 운영자 의도 메모 (textarea) → operator_intents.intent_id 를 cycle 시작 시 받아 저장한다.
-- intent 가 없는 cycle (자동/AI 시작) 은 NULL.

ALTER TABLE feed_cycles ADD COLUMN intent_id TEXT;
CREATE INDEX IF NOT EXISTS idx_feed_cycles_intent ON feed_cycles(intent_id);
