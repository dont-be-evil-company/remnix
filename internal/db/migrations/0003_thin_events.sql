-- 0003_thin_events.sql
-- history_id is filled and history payloads stripped by the Go post-hook.

ALTER TABLE sync_events ADD COLUMN history_id TEXT;
