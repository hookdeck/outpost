BEGIN;

ALTER TABLE event_delivery_index DROP COLUMN IF EXISTS attempt;
ALTER TABLE event_delivery_index DROP COLUMN IF EXISTS manual;

ALTER TABLE deliveries DROP COLUMN IF EXISTS attempt;
ALTER TABLE deliveries DROP COLUMN IF EXISTS manual;

COMMIT;
