ALTER TABLE events ADD COLUMN matched_destination_ids text[] NOT NULL DEFAULT '{}';
CREATE INDEX ON events USING GIN (matched_destination_ids);
DROP INDEX IF EXISTS events_tenant_id_destination_id_idx;
ALTER TABLE events DROP COLUMN destination_id;
