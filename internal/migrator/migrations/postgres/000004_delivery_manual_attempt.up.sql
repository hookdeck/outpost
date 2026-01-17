BEGIN;

ALTER TABLE deliveries ADD COLUMN manual boolean DEFAULT false NOT NULL;
ALTER TABLE deliveries ADD COLUMN attempt integer DEFAULT 0 NOT NULL;

ALTER TABLE event_delivery_index ADD COLUMN manual boolean DEFAULT false NOT NULL;
ALTER TABLE event_delivery_index ADD COLUMN attempt integer DEFAULT 0 NOT NULL;

COMMIT;
