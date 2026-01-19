# pglogstore

PostgreSQL implementation of the LogStore interface.

## Schema

| Table | Purpose | Primary Key |
|-------|---------|-------------|
| `events` | Event data (data, metadata) | `(time, id)` |
| `deliveries` | Delivery attempts (status, response) | `(time, id)` |
| `event_delivery_index` | Query index with cursor columns | `(delivery_time, event_id, delivery_id)` |

All tables are partitioned by time.

## Operations

### ListDeliveryEvent

Query pattern: **Index → Hydrate**

1. CTE filters and paginates on `event_delivery_index`
2. JOIN `events` and `deliveries` by primary key to populate full data

```sql
WITH filtered AS (
    SELECT ... FROM event_delivery_index WHERE [filters] ORDER BY ... LIMIT N
)
SELECT ... FROM filtered f
JOIN events e ON (e.time, e.id) = (f.event_time, f.event_id)
JOIN deliveries d ON (d.time, d.id) = (f.delivery_time, f.delivery_id)
```

**Key considerations:**
- Cursor encodes sort params - rejects mismatched sort order

### RetrieveEvent

Direct lookup by `(tenant_id, event_id)`.

```sql
SELECT id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata
FROM events
WHERE tenant_id = $1 AND id = $2

-- With destination filter:
SELECT id, tenant_id, $3 as destination_id, time, topic, eligible_for_retry, data, metadata
FROM events
WHERE tenant_id = $1 AND id = $2
AND EXISTS (SELECT 1 FROM event_delivery_index WHERE event_id = $2 AND destination_id = $3)
```

### InsertManyDeliveryEvent

Batch insert using `unnest()` arrays in a single transaction across all 3 tables.

```sql
BEGIN;

INSERT INTO events (id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata)
SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::timestamptz[], $5::text[], $6::boolean[], $7::jsonb[], $8::jsonb[])
ON CONFLICT (time, id) DO NOTHING;

INSERT INTO deliveries (id, event_id, destination_id, status, time, code, response_data)
SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::timestamptz[], $6::text[], $7::jsonb[])
ON CONFLICT (time, id) DO UPDATE SET status = EXCLUDED.status, code = EXCLUDED.code, response_data = EXCLUDED.response_data;

INSERT INTO event_delivery_index (event_id, delivery_id, tenant_id, destination_id, event_time, delivery_time, topic, status)
SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::timestamptz[], $6::timestamptz[], $7::text[], $8::text[])
ON CONFLICT (delivery_time, event_id, delivery_id) DO UPDATE SET status = EXCLUDED.status;

COMMIT;
```
