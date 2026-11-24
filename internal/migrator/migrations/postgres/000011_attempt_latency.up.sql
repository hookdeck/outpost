BEGIN;

-- =============================================================================
-- Migration: Add per-attempt latency
--
-- latency_ms is the time the destination took to respond to the attempt (or
-- to fail), measured around the provider's Publish call. It is only aggregated
-- (avg / percentiles on the metrics API), never filtered on, so no index.
--
-- Nullable on purpose: attempts written before this migration have no value,
-- and NULL is skipped by AVG / percentile_cont so historical rows don't drag
-- the numbers down.
-- =============================================================================

ALTER TABLE attempts ADD COLUMN latency_ms integer;

COMMIT;
