BEGIN;

-- =============================================================================
-- Migration: Change response_data column from JSONB to TEXT
--
-- Matches the approach taken in 000006 for events.data and attempts.event_data.
-- Storing response_data as TEXT avoids JSONB key reordering and keeps the
-- serialization boundary in application code rather than the database.
--
-- Columns changed:
--   attempts.response_data (JSONB -> TEXT)
-- =============================================================================

ALTER TABLE attempts ALTER COLUMN response_data TYPE text USING response_data::text;

COMMIT;
