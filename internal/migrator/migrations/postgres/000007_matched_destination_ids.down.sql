ALTER TABLE events ADD COLUMN destination_id text NOT NULL DEFAULT '';
ALTER TABLE events DROP COLUMN matched_destination_ids;
