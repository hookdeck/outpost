ALTER TABLE {deployment_prefix}events ADD COLUMN matched_destination_ids Array(String) DEFAULT [];
ALTER TABLE {deployment_prefix}events ADD INDEX idx_matched_destination_ids matched_destination_ids TYPE bloom_filter GRANULARITY 1;
ALTER TABLE {deployment_prefix}events DROP INDEX IF EXISTS idx_destination_id;
ALTER TABLE {deployment_prefix}events DROP COLUMN IF EXISTS destination_id;
