BEGIN;

-- Drop new index and restore old one
DROP INDEX IF EXISTS idx_event_attempt_index_main;

-- Restore generated column with old name
ALTER TABLE event_attempt_index DROP COLUMN time_attempt_id;
ALTER TABLE event_attempt_index ADD COLUMN time_delivery_id text GENERATED ALWAYS AS (
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

-- Rename columns back in event_attempt_index
ALTER TABLE event_attempt_index RENAME COLUMN attempt_number TO attempt;
ALTER TABLE event_attempt_index RENAME COLUMN attempt_time TO delivery_time;
ALTER TABLE event_attempt_index RENAME COLUMN attempt_id TO delivery_id;

-- Rename tables back
ALTER TABLE event_attempt_index RENAME TO event_delivery_index;
ALTER TABLE event_attempt_index_default RENAME TO event_delivery_index_default;

-- Rename column back in attempts: attempt_number -> attempt
ALTER TABLE attempts RENAME COLUMN attempt_number TO attempt;

ALTER TABLE attempts RENAME TO deliveries;
ALTER TABLE attempts_default RENAME TO deliveries_default;

-- Recreate old index
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

COMMIT;
