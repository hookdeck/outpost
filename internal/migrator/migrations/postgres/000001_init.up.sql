BEGIN;

CREATE TABLE events (
  id text NOT NULL,
  tenant_id text NOT NULL,
  destination_id text NOT NULL,
  time timestamptz NOT NULL,
  topic text NOT NULL,
  eligible_for_retry boolean NOT NULL,
  data jsonb NOT NULL,
  metadata jsonb NOT NULL,
  time_id text GENERATED ALWAYS AS (
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
  ) STORED,
  PRIMARY KEY (time, id)
) PARTITION BY RANGE (time);

CREATE TABLE events_default PARTITION OF events DEFAULT;

CREATE INDEX ON events (tenant_id, time_id DESC);
CREATE INDEX ON events (tenant_id, destination_id);

CREATE TABLE deliveries (
  id text NOT NULL,
  event_id text NOT NULL,
  destination_id text NOT NULL,
  status text NOT NULL,
  time timestamptz NOT NULL,
  PRIMARY KEY (time, id)
) PARTITION BY RANGE (time);

CREATE TABLE deliveries_default PARTITION OF deliveries DEFAULT;

CREATE INDEX ON deliveries (event_id);
CREATE INDEX ON deliveries (event_id, status);

COMMIT;