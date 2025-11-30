-- Single denormalized table for events and deliveries
-- Each row represents a delivery attempt (or pending state) for an event
-- This avoids JOINs and leverages ClickHouse's columnar storage efficiently

CREATE TABLE IF NOT EXISTS event_log (
  -- Event fields
  event_id String,
  tenant_id String,
  destination_id String,
  topic String,
  eligible_for_retry Bool,
  event_time DateTime64(3),
  metadata String,  -- JSON serialized
  data String,      -- JSON serialized

  -- Delivery fields (nullable for pending events)
  delivery_id String,
  delivery_event_id String,
  status String,  -- 'pending', 'success', 'failed'
  delivery_time DateTime64(3),
  code String,
  response_data String,  -- JSON serialized

  -- Indexes for common filter patterns
  INDEX idx_topic topic TYPE bloom_filter GRANULARITY 4,
  INDEX idx_status status TYPE set(100) GRANULARITY 4
) ENGINE = MergeTree
PARTITION BY toYYYYMMDD(event_time)
ORDER BY (tenant_id, destination_id, event_time, event_id, delivery_time);
