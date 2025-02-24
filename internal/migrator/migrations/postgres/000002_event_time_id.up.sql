BEGIN;

ALTER TABLE events
ADD COLUMN time_id TEXT GENERATED ALWAYS AS (
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

CREATE INDEX events_tenant_time_id_idx ON events (tenant_id, time_id DESC);

COMMIT;