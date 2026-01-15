-- Events table for ListEvent queries
-- Each row represents a unique event (ReplacingMergeTree deduplicates by ORDER BY)
-- Enables O(limit) event listing without GROUP BY

CREATE TABLE IF NOT EXISTS events (
    event_id String,
    tenant_id String,
    destination_id String,
    topic String,
    eligible_for_retry Bool,
    event_time DateTime64(3),
    metadata String,      -- JSON serialized
    data String,          -- JSON serialized

    -- Indexes for filtering (bloom filters help skip granules)
    INDEX idx_event_id event_id TYPE bloom_filter GRANULARITY 4,
    INDEX idx_topic topic TYPE bloom_filter GRANULARITY 4
) ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMMDD(event_time)
ORDER BY (tenant_id, destination_id, event_time, event_id);
