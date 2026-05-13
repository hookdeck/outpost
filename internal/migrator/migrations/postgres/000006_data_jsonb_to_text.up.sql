BEGIN;

-- =============================================================================
-- Migration: Change data columns from JSONB to TEXT
--
-- JSONB normalizes JSON key order alphabetically on read, which destroys
-- the original key ordering of webhook payloads. TEXT preserves the raw
-- JSON string exactly as ingested, maintaining key order for delivery.
--
-- Columns changed:
--   events.data       (JSONB -> TEXT)
--   attempts.event_data (JSONB -> TEXT)
-- =============================================================================

ALTER TABLE events ALTER COLUMN data TYPE text USING data::text;
ALTER TABLE attempts ALTER COLUMN event_data TYPE text USING event_data::text;

COMMIT;
