BEGIN;

CREATE TABLE event_delivery_index (
  event_id text NOT NULL,
  delivery_id text NOT NULL,
  tenant_id text NOT NULL,
  destination_id text NOT NULL,
  event_time timestamptz NOT NULL,
  delivery_time timestamptz NOT NULL,
  topic text NOT NULL,
  status text NOT NULL,
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

-- Indexes for common query patterns
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