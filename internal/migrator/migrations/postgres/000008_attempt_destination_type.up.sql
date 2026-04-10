ALTER TABLE attempts ADD COLUMN destination_type text NOT NULL DEFAULT '';
CREATE INDEX idx_attempts_tenant_desttype_time ON attempts (tenant_id, destination_type, time DESC, id DESC);
