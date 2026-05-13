BEGIN;

ALTER TABLE attempts ALTER COLUMN response_data TYPE jsonb USING response_data::jsonb;

COMMIT;
