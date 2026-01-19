-- Events table for ListEvent queries
-- Each row represents a unique event (ReplacingMergeTree deduplicates by ORDER BY)
-- Enables O(limit) event listing without GROUP BY

CREATE TABLE IF NOT EXISTS {deployment_prefix}events (
    event_id String,
    tenant_id String,
    destination_id String,
    topic String,
    eligible_for_retry Bool,
    event_time DateTime64(3),
    metadata String,      -- JSON serialized
    data String,          -- JSON serialized

    -- Indexes for filtering (bloom filters help skip granules)
    INDEX idx_tenant_id tenant_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_destination_id destination_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_event_id event_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_topic topic TYPE bloom_filter GRANULARITY 1
) ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(event_time)
ORDER BY (event_time, event_id);

-- Deliveries table for delivery queries
-- Each row represents a delivery attempt for an event
-- Stateless queries: no GROUP BY, no aggregation, direct row access

CREATE TABLE IF NOT EXISTS {deployment_prefix}deliveries (
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
    manual Bool DEFAULT false,
    attempt UInt32 DEFAULT 0,

    -- Indexes for filtering (bloom filters help skip granules)
    INDEX idx_tenant_id tenant_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_destination_id destination_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_event_id event_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_delivery_id delivery_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_topic topic TYPE bloom_filter GRANULARITY 1,
    INDEX idx_status status TYPE set(100) GRANULARITY 1
) ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(delivery_time)
ORDER BY (delivery_time, delivery_id);
