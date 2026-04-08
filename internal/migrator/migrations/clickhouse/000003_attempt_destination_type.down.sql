ALTER TABLE {deployment_prefix}attempts DROP INDEX IF EXISTS idx_destination_type;
ALTER TABLE {deployment_prefix}attempts DROP COLUMN IF EXISTS destination_type;
