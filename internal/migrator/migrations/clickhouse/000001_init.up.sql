-- Single denormalized table for events and deliveries
-- Each row represents a delivery attempt for an event
-- Stateless queries: no GROUP BY, no aggregation, direct row access

CREATE TABLE IF NOT EXISTS event_log (
    -- Event fields
    event_id String,
    tenant_id String,
    destination_id String,
    topic String,
    eligible_for_retry Bool,
    event_time DateTime64(3),
    metadata String,      -- JSON serialized
    data String,          -- JSON serialized

    -- Delivery fields
    delivery_id String,
    delivery_event_id String,
    status String,        -- 'success', 'failed'
    delivery_time DateTime64(3),
    code String,
    response_data String, -- JSON serialized

    -- Indexes for filtering (bloom filters help skip granules)
    INDEX idx_event_id event_id TYPE bloom_filter GRANULARITY 4,
    INDEX idx_topic topic TYPE bloom_filter GRANULARITY 4,
    INDEX idx_status status TYPE set(100) GRANULARITY 4
) ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMMDD(delivery_time)
ORDER BY (tenant_id, destination_id, delivery_time, event_id, delivery_id);
