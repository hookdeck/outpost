BEGIN;

DROP INDEX IF EXISTS events_tenant_time_id_idx;
ALTER TABLE events DROP COLUMN time_id;

COMMIT;