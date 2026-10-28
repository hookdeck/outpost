ALTER TABLE {deployment_prefix}events MODIFY SETTING ttl_only_drop_parts = 0;
ALTER TABLE {deployment_prefix}attempts MODIFY SETTING ttl_only_drop_parts = 0;
