-- 020_sync_batch_event_seq_index.sql
-- Speeds up sync.createBatch's ListUnsyncedUnbatchedEvents NOT EXISTS lookup.
--
-- sync_batch_events PK is (batch_id, event_sequence); a leftmost-prefix scan on
-- event_sequence falls back to a full table scan. As sync_batch_events grows
-- (every minute the sync worker adds 0-4 rows; with NOT_IMPLEMENTED transport
-- they accumulate), the createBatch query gets progressively slower and holds
-- the only sqlite write connection long enough for API GET handlers to time
-- out behind it.
--
-- Adding a secondary index on event_sequence makes the NOT EXISTS subquery an
-- index lookup instead of a scan, which keeps the connection-holding time
-- bounded regardless of sync_batch_events size.

CREATE INDEX IF NOT EXISTS idx_sync_batch_events_seq
  ON sync_batch_events (event_sequence);
