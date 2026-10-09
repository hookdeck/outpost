ALTER TABLE {deployment_prefix}events ADD COLUMN schema_valid Nullable(Bool);
ALTER TABLE {deployment_prefix}attempts ADD COLUMN schema_valid Nullable(Bool);
