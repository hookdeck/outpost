BEGIN;

-- =============================================================================
-- Migration: Add the message ID to attempts
--
-- message_id is the identifier the attempt's delivery was made under (the
-- delivery task's MessageID, sent as the webhook-id of an MCP event). It is
-- the same on every attempt of one delivery, so a manual retry reads it from
-- the attempt it retries and sends the same message again.
--
-- It is only read with its row, never filtered on, so no index. Attempts
-- written before this migration have '', and a manual retry of one of those
-- gets a new ID.
-- =============================================================================

ALTER TABLE attempts ADD COLUMN message_id text NOT NULL DEFAULT '';

COMMIT;
