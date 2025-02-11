CREATE TABLE IF NOT EXISTS events (
  id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  destination_id TEXT NOT NULL,
  topic TEXT NOT NULL,
  eligible_for_retry BOOLEAN NOT NULL,
  time TIMESTAMPTZ NOT NULL,
  metadata JSONB NOT NULL,
  data JSONB NOT NULL,
  PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS events_tenant_time_idx ON events (tenant_id, time DESC);

CREATE TABLE IF NOT EXISTS deliveries (
  id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  destination_id TEXT NOT NULL,
  status TEXT NOT NULL,
  time TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (id),
  FOREIGN KEY (event_id) REFERENCES events (id)
);

CREATE INDEX IF NOT EXISTS deliveries_event_time_idx ON deliveries (event_id, time DESC);