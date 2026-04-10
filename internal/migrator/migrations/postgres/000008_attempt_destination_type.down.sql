DROP INDEX IF EXISTS idx_attempts_tenant_desttype_time;
ALTER TABLE attempts DROP COLUMN IF EXISTS destination_type;
