BEGIN;

ALTER INDEX IF EXISTS attempts_pkey RENAME TO deliveries_pkey;
ALTER INDEX IF EXISTS attempts_default_pkey RENAME TO deliveries_default_pkey;

COMMIT;
