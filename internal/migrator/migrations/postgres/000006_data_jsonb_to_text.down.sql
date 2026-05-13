BEGIN;

ALTER TABLE events ALTER COLUMN data TYPE jsonb USING data::jsonb;
ALTER TABLE attempts ALTER COLUMN event_data TYPE jsonb USING event_data::jsonb;

COMMIT;
