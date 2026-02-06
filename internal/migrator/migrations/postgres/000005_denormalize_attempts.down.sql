BEGIN;

-- =============================================================================
-- Rollback: Restore v0.12 schema (deliveries + event_delivery_index)
--
-- WARNING: This rollback will NOT restore data deleted during the up migration
-- (orphaned deliveries). The event_delivery_index table will be recreated empty
-- since the original data was buggy anyway.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Step 1: Drop new indexes
-- -----------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_events_tenant_time;
DROP INDEX IF EXISTS idx_events_tenant_topic_time;
DROP INDEX IF EXISTS idx_events_id;
DROP INDEX IF EXISTS idx_attempts_tenant_time;
DROP INDEX IF EXISTS idx_attempts_tenant_dest_time;
DROP INDEX IF EXISTS idx_attempts_tenant_status_time;
DROP INDEX IF EXISTS idx_attempts_tenant_topic_time;
DROP INDEX IF EXISTS idx_attempts_event_time;
DROP INDEX IF EXISTS idx_attempts_id;

-- -----------------------------------------------------------------------------
-- Step 2: Restore time_id generated column on events
-- -----------------------------------------------------------------------------
ALTER TABLE events ADD COLUMN time_id text GENERATED ALWAYS AS (
  LPAD(
    CAST(
      EXTRACT(
        EPOCH
        FROM time AT TIME ZONE 'UTC'
      ) AS BIGINT
    )::text,
    10,
    '0'
  ) || '_' || id
) STORED;

-- -----------------------------------------------------------------------------
-- Step 3: Rename attempts -> deliveries, attempt_number -> attempt
-- -----------------------------------------------------------------------------
ALTER TABLE attempts RENAME COLUMN attempt_number TO attempt;
ALTER TABLE attempts RENAME TO deliveries;
ALTER TABLE attempts_default RENAME TO deliveries_default;

-- -----------------------------------------------------------------------------
-- Step 4: Drop denormalized columns from deliveries
-- -----------------------------------------------------------------------------
ALTER TABLE deliveries
  DROP COLUMN tenant_id,
  DROP COLUMN topic,
  DROP COLUMN event_time,
  DROP COLUMN eligible_for_retry,
  DROP COLUMN event_data,
  DROP COLUMN event_metadata;

-- -----------------------------------------------------------------------------
-- Step 5: Recreate event_delivery_index table (empty - original data was buggy)
-- -----------------------------------------------------------------------------
CREATE TABLE event_delivery_index (
  event_id text NOT NULL,
  delivery_id text NOT NULL,
  tenant_id text NOT NULL,
  destination_id text NOT NULL,
  event_time timestamptz NOT NULL,
  delivery_time timestamptz NOT NULL,
  topic text NOT NULL,
  status text NOT NULL,
  manual boolean DEFAULT false NOT NULL,
  attempt integer DEFAULT 0 NOT NULL,
  time_event_id text GENERATED ALWAYS AS (
    LPAD(
      CAST(
        EXTRACT(
          EPOCH
          FROM event_time AT TIME ZONE 'UTC'
        ) AS BIGINT
      )::text,
      10,
      '0'
    ) || '_' || event_id
  ) STORED,
  time_delivery_id text GENERATED ALWAYS AS (
    LPAD(
      CAST(
        EXTRACT(
          EPOCH
          FROM delivery_time AT TIME ZONE 'UTC'
        ) AS BIGINT
      )::text,
      10,
      '0'
    ) || '_' || delivery_id
  ) STORED,
  PRIMARY KEY (delivery_time, event_id, delivery_id)
) PARTITION BY RANGE (delivery_time);

CREATE TABLE event_delivery_index_default PARTITION OF event_delivery_index DEFAULT;

CREATE INDEX IF NOT EXISTS idx_event_delivery_index_main ON event_delivery_index(
  tenant_id,
  destination_id,
  topic,
  status,
  event_time DESC,
  delivery_time DESC,
  time_event_id,
  time_delivery_id
);

-- -----------------------------------------------------------------------------
-- Step 6: Restore old indexes on events and deliveries
-- -----------------------------------------------------------------------------
CREATE INDEX ON events (tenant_id, time_id DESC);
CREATE INDEX ON events (tenant_id, destination_id);
CREATE INDEX ON deliveries (event_id);
CREATE INDEX ON deliveries (event_id, status);

COMMIT;
