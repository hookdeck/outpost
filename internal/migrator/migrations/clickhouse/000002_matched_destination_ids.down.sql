ALTER TABLE {deployment_prefix}events ADD COLUMN destination_id String DEFAULT '';
ALTER TABLE {deployment_prefix}events ADD INDEX idx_destination_id destination_id TYPE bloom_filter GRANULARITY 1;
ALTER TABLE {deployment_prefix}events DROP INDEX IF EXISTS idx_matched_destination_ids;
ALTER TABLE {deployment_prefix}events DROP COLUMN IF EXISTS matched_destination_ids;
