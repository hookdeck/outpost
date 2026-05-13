BEGIN;

-- =============================================================================
-- Migration: Restructure for denormalized attempts table
--
-- Changes:
-- 1. Add denormalized event columns to deliveries
-- 2. Backfill data from events table
-- 3. Rename deliveries -> attempts
-- 4. Drop buggy event_delivery_index table (delivery_id was stored incorrectly)
-- 5. Drop time_id from events (cursor now computed dynamically)
-- 6. Create new indexes optimized for the query patterns
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Step 1: Add denormalized columns to deliveries
-- -----------------------------------------------------------------------------
ALTER TABLE deliveries
  ADD COLUMN tenant_id text,
  ADD COLUMN topic text,
  ADD COLUMN event_time timestamptz,
  ADD COLUMN eligible_for_retry boolean,
  ADD COLUMN event_data jsonb,
  ADD COLUMN event_metadata jsonb;

-- -----------------------------------------------------------------------------
-- Step 2: Backfill denormalized data from events
-- Note: event_delivery_index has buggy data (delivery_id stored incorrectly),
-- so we backfill directly from events table using event_id foreign key.
-- -----------------------------------------------------------------------------
UPDATE deliveries d SET
  tenant_id = e.tenant_id,
  topic = e.topic,
  event_time = e.time,
  eligible_for_retry = e.eligible_for_retry,
  event_data = e.data,
  event_metadata = e.metadata
FROM events e
WHERE d.event_id = e.id;

-- Handle any orphaned deliveries (delivery exists but event doesn't)
-- These would have NULL values - delete them as they're invalid
DELETE FROM deliveries WHERE tenant_id IS NULL;

-- -----------------------------------------------------------------------------
-- Step 3: Make denormalized columns NOT NULL
-- -----------------------------------------------------------------------------
ALTER TABLE deliveries
  ALTER COLUMN tenant_id SET NOT NULL,
  ALTER COLUMN topic SET NOT NULL,
  ALTER COLUMN event_time SET NOT NULL,
  ALTER COLUMN eligible_for_retry SET NOT NULL,
  ALTER COLUMN event_data SET NOT NULL,
  ALTER COLUMN event_metadata SET NOT NULL;

-- -----------------------------------------------------------------------------
-- Step 4: Rename deliveries -> attempts, attempt -> attempt_number
-- -----------------------------------------------------------------------------
ALTER TABLE deliveries RENAME TO attempts;
ALTER TABLE deliveries_default RENAME TO attempts_default;
ALTER TABLE attempts RENAME COLUMN attempt TO attempt_number;

-- -----------------------------------------------------------------------------
-- Step 5: Drop buggy event_delivery_index table
-- The delivery_id column was storing incorrect values, making this table
-- unreliable. The denormalized attempts table replaces its functionality.
-- -----------------------------------------------------------------------------
DROP TABLE event_delivery_index CASCADE;

-- -----------------------------------------------------------------------------
-- Step 6: Drop time_id from events
-- Cursor position is now computed dynamically as {time_ms}::{id} to match
-- chlogstore format, rather than stored as a generated column.
-- -----------------------------------------------------------------------------
ALTER TABLE events DROP COLUMN time_id;

-- -----------------------------------------------------------------------------
-- Step 7: Drop old indexes
-- -----------------------------------------------------------------------------
DROP INDEX IF EXISTS events_tenant_id_time_id_idx;
DROP INDEX IF EXISTS events_tenant_id_destination_id_idx;
DROP INDEX IF EXISTS deliveries_event_id_idx;
DROP INDEX IF EXISTS deliveries_event_id_status_idx;

-- -----------------------------------------------------------------------------
-- Step 8: Create new indexes for events
-- -----------------------------------------------------------------------------
-- Primary list query: paginate by (time, id) with tenant filter
CREATE INDEX idx_events_tenant_time ON events (tenant_id, time DESC, id DESC);

-- Filter by topic
CREATE INDEX idx_events_tenant_topic_time ON events (tenant_id, topic, time DESC, id DESC);

-- Point lookup by event_id (scans partition indexes)
CREATE INDEX idx_events_id ON events (id);

-- -----------------------------------------------------------------------------
-- Step 9: Create new indexes for attempts
-- -----------------------------------------------------------------------------
-- Primary list query: paginate by (time, id) with tenant filter
CREATE INDEX idx_attempts_tenant_time ON attempts (tenant_id, time DESC, id DESC);

-- Filter by destination
CREATE INDEX idx_attempts_tenant_dest_time ON attempts (tenant_id, destination_id, time DESC, id DESC);

-- Filter by status
CREATE INDEX idx_attempts_tenant_status_time ON attempts (tenant_id, status, time DESC, id DESC);

-- Filter by topic
CREATE INDEX idx_attempts_tenant_topic_time ON attempts (tenant_id, topic, time DESC, id DESC);

-- Filter by event_id (for "attempts for this event" queries)
CREATE INDEX idx_attempts_event_time ON attempts (event_id, time DESC, id DESC);

-- Point lookup by attempt_id (scans partition indexes)
CREATE INDEX idx_attempts_id ON attempts (id);

COMMIT;
