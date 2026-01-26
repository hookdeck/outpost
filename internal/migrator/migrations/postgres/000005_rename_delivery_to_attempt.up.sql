BEGIN;

-- Rename deliveries table to attempts
ALTER TABLE deliveries RENAME TO attempts;
ALTER TABLE deliveries_default RENAME TO attempts_default;

-- Rename column in attempts: attempt -> attempt_number
ALTER TABLE attempts RENAME COLUMN attempt TO attempt_number;

-- Rename event_delivery_index table to event_attempt_index
ALTER TABLE event_delivery_index RENAME TO event_attempt_index;
ALTER TABLE event_delivery_index_default RENAME TO event_attempt_index_default;

-- Rename columns in event_attempt_index
ALTER TABLE event_attempt_index RENAME COLUMN delivery_id TO attempt_id;
ALTER TABLE event_attempt_index RENAME COLUMN delivery_time TO attempt_time;
ALTER TABLE event_attempt_index RENAME COLUMN attempt TO attempt_number;

-- Drop and recreate generated column with new name
ALTER TABLE event_attempt_index DROP COLUMN time_delivery_id;
ALTER TABLE event_attempt_index ADD COLUMN time_attempt_id text GENERATED ALWAYS AS (
  LPAD(
    CAST(
      EXTRACT(
        EPOCH
        FROM attempt_time AT TIME ZONE 'UTC'
      ) AS BIGINT
    )::text,
    10,
    '0'
  ) || '_' || attempt_id
) STORED;

-- Drop old index and create new one with updated column names
DROP INDEX IF EXISTS idx_event_delivery_index_main;
CREATE INDEX IF NOT EXISTS idx_event_attempt_index_main ON event_attempt_index(
  tenant_id,
  destination_id,
  topic,
  status,
  event_time DESC,
  attempt_time DESC,
  time_event_id,
  time_attempt_id
);

COMMIT;
