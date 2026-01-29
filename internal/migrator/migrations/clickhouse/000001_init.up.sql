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

-- Attempts table for attempt queries
-- Each row represents an attempt for an event
-- Stateless queries: no GROUP BY, no aggregation, direct row access

CREATE TABLE IF NOT EXISTS {deployment_prefix}attempts (
    -- Event fields
    event_id String,
    tenant_id String,
    destination_id String,
    topic String,
    eligible_for_retry Bool,
    event_time DateTime64(3),
    metadata String,      -- JSON serialized
    data String,          -- JSON serialized

    -- Attempt fields
    attempt_id String,
    status String,        -- 'success', 'failed'
    attempt_time DateTime64(3),
    code String,
    response_data String, -- JSON serialized
    manual Bool DEFAULT false,
    attempt_number UInt32 DEFAULT 0,

    -- Indexes for filtering (bloom filters help skip granules)
    INDEX idx_tenant_id tenant_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_destination_id destination_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_event_id event_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_attempt_id attempt_id TYPE bloom_filter GRANULARITY 1,
    INDEX idx_topic topic TYPE bloom_filter GRANULARITY 1,
    INDEX idx_status status TYPE set(100) GRANULARITY 1
) ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(attempt_time)
ORDER BY (attempt_time, attempt_id);
