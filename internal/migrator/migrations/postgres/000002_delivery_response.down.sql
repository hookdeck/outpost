BEGIN;

ALTER TABLE deliveries DROP COLUMN code,
  DROP COLUMN response_data;

COMMIT;