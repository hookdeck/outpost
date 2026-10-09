ALTER TABLE {deployment_prefix}attempts DROP COLUMN IF EXISTS schema_valid;
ALTER TABLE {deployment_prefix}events DROP COLUMN IF EXISTS schema_valid;
