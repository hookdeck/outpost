# chlogstore

ClickHouse implementation of the LogStore interface.

## Schema

Single denormalized table - each row represents a delivery attempt for an event.

| Table | Engine | Partition | Order By |
|-------|--------|-----------|----------|
| `event_log` | ReplacingMergeTree | `toYYYYMMDD(delivery_time)` | `(tenant_id, destination_id, delivery_time, event_id, delivery_id)` |

**Secondary indexes:**
- `idx_event_id` - bloom_filter for event_id lookups
- `idx_topic` - bloom_filter for topic filtering
- `idx_status` - set index for status filtering

## Design Principles

### Stateless Queries

All queries are designed to be stateless:
- No `GROUP BY`, no aggregation
- Direct row access with `ORDER BY` + `LIMIT`
- O(limit) performance regardless of total data volume

This avoids the scaling issues of aggregation-based queries that must scan all matching rows before applying LIMIT.

### Eventual Consistency

ReplacingMergeTree deduplicates rows asynchronously during background merges. This means:
- Duplicate inserts (retries) may briefly appear as multiple rows
- Background merge consolidates duplicates within seconds/minutes
- Production queries do NOT use `FINAL` to avoid full-scan overhead

For most use cases (log viewing), brief duplicates are acceptable.

## Operations

### ListDeliveryEvent

Direct index scan with cursor-based pagination.

```sql
SELECT
    event_id, tenant_id, destination_id, topic, eligible_for_retry,
    event_time, metadata, data,
    delivery_id, delivery_event_id, status, delivery_time, code, response_data
FROM event_log
WHERE tenant_id = ?
    AND [optional filters: destination_id, status, topic, time ranges]
    AND [cursor condition]
ORDER BY delivery_time DESC, delivery_id DESC
LIMIT 101
```

**Cursor design:**
- Format: `v1:{sortBy}:{sortOrder}:{position}` (base62 encoded)
- Position for delivery_time sort: `{timestamp}::{delivery_id}`
- Position for event_time sort: `{event_timestamp}::{event_id}::{delivery_timestamp}::{delivery_id}`
- Validates sort params match - rejects mismatched cursors

**Backward pagination:**
- Reverses ORDER BY direction
- Reverses comparison operator in cursor condition
- Reverses results after fetching

### RetrieveEvent

Direct lookup by tenant_id and event_id.

```sql
SELECT
    event_id, tenant_id, destination_id, topic, eligible_for_retry,
    event_time, metadata, data
FROM event_log
WHERE tenant_id = ? AND event_id = ?
LIMIT 1
```

With destination filter, adds `AND destination_id = ?`.

### InsertManyDeliveryEvent

Batch insert using ClickHouse's native batch API.

```go
batch, _ := conn.PrepareBatch(ctx, `
    INSERT INTO event_log (
        event_id, tenant_id, destination_id, topic, eligible_for_retry,
        event_time, metadata, data,
        delivery_id, delivery_event_id, status, delivery_time, code, response_data
    )
`)
for _, de := range deliveryEvents {
    batch.Append(...)
}
batch.Send()
```

**Idempotency:** ReplacingMergeTree deduplicates rows with identical ORDER BY keys during background merges.

## Performance Characteristics

| Operation | Complexity | Notes |
|-----------|------------|-------|
| ListDeliveryEvent | O(limit) | Index scan, stops at LIMIT |
| RetrieveEvent | O(1) | Single row lookup via bloom filter |
| InsertManyDeliveryEvent | O(batch) | Batch insert, async dedup |
