BEGIN;

-- =============================================================================
-- Migration: Rename the stale deliveries_* primary key indexes to attempts_*
--
-- 000005 renamed the deliveries tables to attempts, but Postgres does not
-- rename dependent indexes and constraints on a table rename, so the primary
-- key of attempts (and of its default partition) still carries the old name.
--
-- Indexes renamed:
--   deliveries_pkey         -> attempts_pkey
--   deliveries_default_pkey -> attempts_default_pkey
-- =============================================================================

ALTER INDEX IF EXISTS deliveries_pkey RENAME TO attempts_pkey;
ALTER INDEX IF EXISTS deliveries_default_pkey RENAME TO attempts_default_pkey;

COMMIT;
