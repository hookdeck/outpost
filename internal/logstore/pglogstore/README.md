# pglogstore

PostgreSQL implementation of the LogStore interface.

## Schema

Denormalized design optimized for read-heavy workloads. Events are stored once; attempts embed event data for JOIN-free queries.

| Table | Purpose | Primary Key |
|-------|---------|-------------|
| `events` | Event payload (for ListEvent, RetrieveEvent) | `(time, id)` |
| `attempts` | Delivery attempts with embedded event data | `(time, id)` |

Both tables are partitioned by time for efficient data management and pruning.

### events

```sql
CREATE TABLE events (
    id text NOT NULL,
    tenant_id text NOT NULL,
    destination_id text NOT NULL,  -- publish input (NOT matched destinations)
    time timestamptz NOT NULL,
    topic text NOT NULL,
    eligible_for_retry boolean NOT NULL,
    data jsonb NOT NULL,
    metadata jsonb NOT NULL,
    PRIMARY KEY (time, id)  -- partition key must be in PK
) PARTITION BY RANGE (time);

-- Primary list query: paginate by (time, id)
CREATE INDEX idx_events_tenant_time ON events (tenant_id, time DESC, id DESC);

-- Filter by topic
CREATE INDEX idx_events_tenant_topic_time ON events (tenant_id, topic, time DESC, id DESC);

-- Point lookup by event_id (scans partition indexes, not O(1))
CREATE INDEX idx_events_id ON events (id);
```

> **Note:** Events are destination-agnostic. The `destination_id` field represents the publish input
> (explicit destination targeting), not the destinations that matched via routing rules. To filter
> by destination, use `ListAttempt` which queries actual delivery attempts.

### attempts

```sql
CREATE TABLE attempts (
    id text NOT NULL,
    event_id text NOT NULL,
    tenant_id text NOT NULL,
    destination_id text NOT NULL,
    topic text NOT NULL,
    status text NOT NULL,
    time timestamptz NOT NULL,
    attempt_number integer NOT NULL,
    manual boolean NOT NULL DEFAULT false,
    code text,
    response_data jsonb,
    -- Embedded event data (denormalized)
    event_time timestamptz NOT NULL,
    eligible_for_retry boolean NOT NULL,
    event_data jsonb NOT NULL,
    event_metadata jsonb NOT NULL,
    PRIMARY KEY (time, id)  -- partition key must be in PK
) PARTITION BY RANGE (time);

-- Primary list query: paginate by (time, id)
CREATE INDEX idx_attempts_tenant_time ON attempts (tenant_id, time DESC, id DESC);

-- Filter by destination
CREATE INDEX idx_attempts_tenant_dest_time ON attempts (tenant_id, destination_id, time DESC, id DESC);

-- Filter by status
CREATE INDEX idx_attempts_tenant_status_time ON attempts (tenant_id, status, time DESC, id DESC);

-- Filter by topic
CREATE INDEX idx_attempts_tenant_topic_time ON attempts (tenant_id, topic, time DESC, id DESC);

-- Filter by event_id (for "attempts for this event" queries)
CREATE INDEX idx_attempts_event_time ON attempts (event_id, time DESC, id DESC);

-- Point lookup by attempt_id (scans partition indexes, not O(1))
CREATE INDEX idx_attempts_id ON attempts (id);
```

## Cursor Format

Cursors encode pagination position as `{time_ms}::{id}` (matching chlogstore):

```go
// Encode: time milliseconds + "::" + id
position := fmt.Sprintf("%d::%s", event.Time.UnixMilli(), event.ID)
cursor := cursor.Encode("evt", 1, position)  // base62 encoded

// Decode: parse back to time and id
position, _ := cursor.Decode(encoded, "evt", 1)
parts := strings.SplitN(position, "::", 2)
timeMs, _ := strconv.ParseInt(parts[0], 10, 64)
id := parts[1]
```

Resources:
- Events: `evt` prefix
- Attempts: `att` prefix

---

## Operations

### ListEvent

Paginated list of events with optional filters.

**Filters:** tenant_id (optional), topic[], time range
**Note:** destination_id[] filter returns unimplemented error (events are destination-agnostic; use ListAttempt instead)
**Pagination:** bidirectional cursor on `(time, id)`

```sql
SELECT id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata
FROM events
WHERE ($1 = '' OR tenant_id = $1)                                    -- optional tenant filter
  AND ($2::text[] IS NULL OR cardinality($2) = 0 OR topic = ANY($2)) -- optional topic filter
  AND ($3::timestamptz IS NULL OR time >= $3)  -- GTE
  AND ($4::timestamptz IS NULL OR time <= $4)  -- LTE
  AND ($5::timestamptz IS NULL OR time > $5)   -- GT
  AND ($6::timestamptz IS NULL OR time < $6)   -- LT
  -- Cursor condition (expanded OR form for clarity):
  AND (
    time < $cursor_time
    OR (time = $cursor_time AND id < $cursor_id)
  )
ORDER BY time DESC, id DESC
LIMIT $limit + 1;
```

**Bidirectional Pagination:**
- **Next cursor**: `time < $t OR (time = $t AND id < $id)` with `ORDER BY time DESC, id DESC`
- **Prev cursor**: `time > $t OR (time = $t AND id > $id)` with `ORDER BY time ASC, id ASC`, then reverse results

| Scenario | Index Used | Performance |
|----------|-----------|-------------|
| No filters | `idx_events_tenant_time` | O(limit) - index scan |
| topic filter | `idx_events_tenant_topic_time` | O(limit) - index scan |
| topic[] (multiple) | `idx_events_tenant_time` + filter | O(N) - scans then filters |

#### Performance Notes

- **Single topic filter**: Index seek, O(limit)
- **Array filters** (`topic = ANY($2)`): Falls back to tenant+time index with in-memory filtering

---

### ListAttempt

Paginated list of attempts with embedded event data. No JOINs required.

**Filters:** tenant_id (optional), event_id, destination_id[], status, topic[], time range
**Pagination:** bidirectional cursor on `(time, id)`

```sql
SELECT
    id, event_id, tenant_id, destination_id, topic, status, time,
    attempt_number, manual, code, response_data,
    event_time, eligible_for_retry, event_data, event_metadata
FROM attempts
WHERE ($1 = '' OR tenant_id = $1)                                              -- optional tenant filter
  AND ($2::text = '' OR event_id = $2)
  AND ($3::text[] IS NULL OR cardinality($3) = 0 OR destination_id = ANY($3))  -- optional destination filter
  AND ($4::text = '' OR status = $4)
  AND ($5::text[] IS NULL OR cardinality($5) = 0 OR topic = ANY($5))           -- optional topic filter
  AND ($6::timestamptz IS NULL OR time >= $6)  -- GTE
  AND ($7::timestamptz IS NULL OR time <= $7)  -- LTE
  AND ($8::timestamptz IS NULL OR time > $8)   -- GT
  AND ($9::timestamptz IS NULL OR time < $9)   -- LT
  -- Cursor condition (expanded OR form for clarity):
  AND (
    time < $cursor_time
    OR (time = $cursor_time AND id < $cursor_id)
  )
ORDER BY time DESC, id DESC
LIMIT $limit + 1;
```

**Bidirectional Pagination:**
- **Next cursor**: `time < $t OR (time = $t AND id < $id)` with `ORDER BY time DESC, id DESC`
- **Prev cursor**: `time > $t OR (time = $t AND id > $id)` with `ORDER BY time ASC, id ASC`, then reverse results

| Scenario | Index Used | Performance |
|----------|-----------|-------------|
| No filters | `idx_attempts_tenant_time` | O(limit) - index scan |
| event_id filter | `idx_attempts_event_time` | O(limit) - index scan |
| destination_id filter | `idx_attempts_tenant_dest_time` | O(limit) - index scan |
| status filter | `idx_attempts_tenant_status_time` | O(limit) - index scan |
| topic filter | `idx_attempts_tenant_topic_time` | O(limit) - index scan |
| **Multiple filters combined** | **Bitmap AND or sequential filter** | **O(N) worst case** |

#### Performance Notes

- **Single filter queries** are O(limit) with appropriate index
- **event_id queries** ("all attempts for event X") are fast via dedicated index
- **status filter** is highly selective (only 2 values: success/failed) - index is effective

**Slow Query Warning:**
```sql
-- Combining destination[] + status + topic[]
-- No single index covers this; PostgreSQL may:
-- 1. Bitmap AND multiple indexes (moderate)
-- 2. Scan tenant_time index and filter (slow)
WHERE destination_id = ANY($2) AND status = $3 AND topic = ANY($4)
```

---

### RetrieveEvent

Point lookup by event ID.

```sql
-- Simple lookup (tenant_id optional)
-- Note: Uses idx_events_id, scans across partitions
SELECT id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata
FROM events
WHERE ($1 = '' OR tenant_id = $1) AND id = $2;

-- With destination filter (verify event was sent to this destination)
SELECT id, tenant_id, $3 as destination_id, time, topic, eligible_for_retry, data, metadata
FROM events
WHERE ($1 = '' OR tenant_id = $1) AND id = $2
  AND EXISTS (SELECT 1 FROM attempts WHERE event_id = $2 AND destination_id = $3 LIMIT 1);
```

| Scenario | Index Used | Performance |
|----------|-----------|-------------|
| By ID | idx_events_id | O(partitions) - scans each partition's index |
| With destination check | idx_events_id + idx_attempts_event_time | O(partitions) |

**Performance Note:** With time-based partitioning, lookups by ID alone must scan the index in each partition since ID is not the partition key. This is acceptable for point lookups but not ideal. If performance becomes an issue, consider requiring time hints or using a separate ID→time mapping.

---

### RetrieveAttempt

Point lookup by attempt ID.

```sql
-- tenant_id optional
-- Note: Uses idx_attempts_id, scans across partitions
SELECT
    id, event_id, tenant_id, destination_id, topic, status, time,
    attempt_number, manual, code, response_data,
    event_time, eligible_for_retry, event_data, event_metadata
FROM attempts
WHERE ($1 = '' OR tenant_id = $1) AND id = $2;
```

| Scenario | Index Used | Performance |
|----------|-----------|-------------|
| By ID | idx_attempts_id | O(partitions) - scans each partition's index |

**Performance Note:** Same as RetrieveEvent - lookups by ID scan partition indexes.

---

### InsertMany

Batch insert events and attempts in a single transaction.

```sql
BEGIN;

-- Insert events (deduplicated by caller)
INSERT INTO events (id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata)
SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::timestamptz[], $5::text[], $6::boolean[], $7::jsonb[], $8::jsonb[])
ON CONFLICT (time, id) DO NOTHING;  -- PK is (time, id)

-- Insert attempts with embedded event data
INSERT INTO attempts (
    id, event_id, tenant_id, destination_id, topic, status, time,
    attempt_number, manual, code, response_data,
    event_time, eligible_for_retry, event_data, event_metadata
)
SELECT * FROM unnest(
    $1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::timestamptz[],
    $8::integer[], $9::boolean[], $10::text[], $11::jsonb[],
    $12::timestamptz[], $13::boolean[], $14::jsonb[], $15::jsonb[]
)
ON CONFLICT (time, id) DO UPDATE SET  -- PK is (time, id)
    status = EXCLUDED.status,
    code = EXCLUDED.code,
    response_data = EXCLUDED.response_data;

COMMIT;
```

| Scenario | Performance |
|----------|-------------|
| Batch insert N events + M attempts | O(N + M) - linear with batch size |

**Performance:** Efficient. `unnest()` avoids N round-trips. Single transaction ensures atomicity.

---

## Performance Summary

### Fast Queries (O(limit))

| Operation | Condition |
|-----------|-----------|
| ListEvent | Single filter or no filter |
| ListAttempt | Single filter or no filter |
| ListAttempt | event_id filter (dedicated index) |
| InsertMany | Always (O(batch size)) |

### Moderate Queries (O(partitions))

| Operation | Condition | Notes |
|-----------|-----------|-------|
| RetrieveEvent | By ID | Scans idx_events_id across partitions |
| RetrieveAttempt | By ID | Scans idx_attempts_id across partitions |

### Potentially Slow Queries (O(N))

| Operation | Condition | Mitigation |
|-----------|-----------|------------|
| ListEvent | `topic = ANY(...)` with multiple values | Accept; rare use case |
| ListAttempt | Combined destination[] + status + topic[] | Bitmap index intersection helps |

### Index Overhead

Total indexes per table:
- **events:** 4 indexes (PK + 2 composite + 1 id lookup)
- **attempts:** 7 indexes (PK + 5 composite + 1 id lookup)

Write amplification is acceptable for read-heavy workload. Each INSERT touches all indexes, but batch inserts amortize overhead.

---

## Storage Considerations

Denormalization trades storage for query performance:

| Field | Duplicated In |
|-------|--------------|
| event_time | Every attempt |
| eligible_for_retry | Every attempt |
| event_data (jsonb) | Every attempt |
| event_metadata (jsonb) | Every attempt |

**Estimate:** If avg event payload is 2KB and avg attempts/event is 1.5, storage overhead is ~50% vs normalized schema.

**Justification:** Events are immutable; no update anomalies. Storage is cheap; query latency is not.

---

## Partitioning

Both tables partition by time (monthly recommended):

```sql
CREATE TABLE events_2024_01 PARTITION OF events
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');

CREATE TABLE attempts_2024_01 PARTITION OF attempts
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
```

**Benefits:**
- Partition pruning for time-filtered queries
- Easy data retention (drop old partitions)
- Parallel query across partitions

**Cursor consideration:** Cursors encode `(time, id)`. Time-based partitioning aligns with cursor pagination, enabling partition pruning during pagination.
