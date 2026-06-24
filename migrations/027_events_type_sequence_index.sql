-- 027_events_type_sequence_index.sql
-- Speeds up the "latest N events of a given event_type" pattern used pervasively
-- by tank_state_vector collect functions (Equipment, Confidence, Fish, etc.).
--
-- The existing idx_events_type_time covers (event_type, recorded_at), but the
-- runtime query is `WHERE event_type=? ORDER BY sequence DESC LIMIT N`. With
-- only idx_events_type_time, SQLite resolves WHERE via the index but performs
-- a TEMP B-TREE sort over every matching row to satisfy ORDER BY sequence DESC.
-- For high-volume event types (device.health.updated, sensor.reading.recorded
-- — tens of thousands of rows in normal operation), this sort dominates: a
-- LIMIT 50 query that should take ~2 ms blows up to 30 ms standalone, and to
-- seconds under concurrent reader contention.
--
-- This composite index lets the planner serve the WHERE filter and the ORDER
-- BY in one descending index walk, eliminating the temp sort entirely.

CREATE INDEX IF NOT EXISTS idx_events_type_seq
  ON events(event_type, sequence DESC);
