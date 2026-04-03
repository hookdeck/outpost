ALTER TABLE {deployment_prefix}attempts ADD COLUMN destination_type String DEFAULT '';
ALTER TABLE {deployment_prefix}attempts ADD INDEX idx_destination_type destination_type TYPE bloom_filter GRANULARITY 1;
