BEGIN;

-- =============================================================================
-- Migration: Add the publish-time schema validation verdict
--
-- schema_valid is true or false when the event's topic validates its data
-- against a payload schema (false only in warn mode, since enforce rejects the
-- publish), and NULL when the data was not checked. attempts carries a copy
-- because retries rebuild the event from the latest attempt row.
--
-- Nullable with no default, so adding it is catalog-only on both partitioned
-- tables (no rewrite), and rows written before this migration read as not
-- checked. No index: the column is returned, never filtered on.
-- =============================================================================

ALTER TABLE events ADD COLUMN schema_valid boolean;
ALTER TABLE attempts ADD COLUMN schema_valid boolean;

COMMIT;
