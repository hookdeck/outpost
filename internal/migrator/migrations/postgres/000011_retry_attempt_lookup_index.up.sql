-- Support retry scheduler lookups for the latest attempt by event, tenant, and
-- destination. This is created on the default partition because PostgreSQL does
-- not support CREATE INDEX CONCURRENTLY on a partitioned parent table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS attempts_default_event_tenant_destination_time_id_idx
ON attempts_default (event_id, tenant_id, destination_id, time DESC, id DESC);
