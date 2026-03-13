-- seed.sql — Deterministic bulk seeding for PG metrics benchmarks.
--
-- Usage:
--   psql "$POSTGRES_URL" -v ROWS=10000000 -f cmd/bench/metrics/pg/seed.sql
--
-- Default :ROWS = 10000000 (10M). Override with -v ROWS=N.
--
-- Distribution:
--   2 tenants: tenant_0 gets 90%, tenant_1 gets 10%
--   Time: evenly spread across January 2000 (2000-01-01 to 2000-02-01)
--   No explicit partitions — data lands in the default partition.
--
-- Attempt chain (1 event → 1-4 attempts):
--   attempt 0: all events.       Failed if n%5=0   (20%)
--   attempt 1: failed attempt 0. Failed if n%20=0  (25% of retries)
--   attempt 2: failed attempt 1. Failed if n%100=0 (20% of retries)
--   attempt 3: failed attempt 2. Failed if n%200=0 (50% of retries)
--
--   For 10M events → ~12.6M attempts. 0.5% events permanently failed.

\set ON_ERROR_STOP on
\timing on

-- Default if not supplied via -v
SELECT COALESCE(:'ROWS', '10000000') AS rows_count \gset

\echo Seeding :rows_count events + chained attempts...

-- ============================================================================
-- 1. Bulk INSERT into events
-- ============================================================================
--
-- Tenants:   n%10 == 0 → tenant_1 (10%), else tenant_0 (90%)
-- Destinations: dest_(n%500)                   [500 destinations]
-- Topics:    n%3 → order.created / order.updated / payment.received
-- Time:      Even spread across 2000-01-01 to 2000-02-01
-- eligible_for_retry: n%3 != 2

\echo [1/7] Inserting events...

INSERT INTO events (id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata)
SELECT
  'evt_' || n                                           AS id,
  CASE WHEN n % 10 = 0
    THEN 'tenant_1'
    ELSE 'tenant_0'
  END                                                   AS tenant_id,
  'dest_' || (n % 500)                                  AS destination_id,
  '2000-01-01'::timestamptz
    + (n::double precision / :'rows_count'::double precision)
    * ('2000-02-01'::timestamptz - '2000-01-01'::timestamptz)
                                                        AS time,
  CASE n % 3
    WHEN 0 THEN 'order.created'
    WHEN 1 THEN 'order.updated'
    ELSE 'payment.received'
  END                                                   AS topic,
  (n % 3 != 2)                                          AS eligible_for_retry,
  '{}'                                                  AS data,
  '{}'::jsonb                                           AS metadata
FROM generate_series(0, :'rows_count'::int - 1) AS n;

-- ============================================================================
-- 2. Bulk INSERT into attempts (chained retries)
-- ============================================================================
--
-- Shared columns reuse the same expressions as events.
-- Each attempt's time = event_time + (attempt_number * 1 second).
-- manual: only attempt_number >= 3 AND n%10=9 (10% of late retries).
-- Code: success→200/201, failed→500/422 (alternating on n%2).

\echo [2/7] Inserting attempt 1 (all events)...

INSERT INTO attempts (
  id, event_id, tenant_id, destination_id, topic, status, time,
  attempt_number, manual, code, response_data,
  event_time, eligible_for_retry, event_data, event_metadata
)
SELECT
  'att_' || n || '_0'                                   AS id,
  'evt_' || n                                           AS event_id,
  CASE WHEN n % 10 = 0 THEN 'tenant_1' ELSE 'tenant_0' END
                                                        AS tenant_id,
  'dest_' || (n % 500)                                  AS destination_id,
  CASE n % 3
    WHEN 0 THEN 'order.created'
    WHEN 1 THEN 'order.updated'
    ELSE 'payment.received'
  END                                                   AS topic,
  CASE WHEN n % 5 = 0 THEN 'failed' ELSE 'success' END AS status,
  '2000-01-01'::timestamptz
    + (n::double precision / :'rows_count'::double precision)
    * ('2000-02-01'::timestamptz - '2000-01-01'::timestamptz)
    + interval '1 second'
                                                        AS time,
  1                                                     AS attempt_number,
  false                                                 AS manual,
  CASE
    WHEN n % 5 != 0 THEN CASE WHEN n%2=0 THEN '200' ELSE '201' END
    ELSE                  CASE WHEN n%2=0 THEN '500' ELSE '422' END
  END                                                   AS code,
  NULL                                                  AS response_data,
  '2000-01-01'::timestamptz
    + (n::double precision / :'rows_count'::double precision)
    * ('2000-02-01'::timestamptz - '2000-01-01'::timestamptz)
                                                        AS event_time,
  (n % 3 != 2)                                          AS eligible_for_retry,
  '{}'                                                  AS event_data,
  '{}'::jsonb                                           AS event_metadata
FROM generate_series(0, :'rows_count'::int - 1) AS n;

\echo [3/7] Inserting attempt 2 (20% of events)...

INSERT INTO attempts (
  id, event_id, tenant_id, destination_id, topic, status, time,
  attempt_number, manual, code, response_data,
  event_time, eligible_for_retry, event_data, event_metadata
)
SELECT
  'att_' || n || '_1'                                   AS id,
  'evt_' || n                                           AS event_id,
  CASE WHEN n % 10 = 0 THEN 'tenant_1' ELSE 'tenant_0' END
                                                        AS tenant_id,
  'dest_' || (n % 500)                                  AS destination_id,
  CASE n % 3
    WHEN 0 THEN 'order.created'
    WHEN 1 THEN 'order.updated'
    ELSE 'payment.received'
  END                                                   AS topic,
  CASE WHEN n % 20 = 0 THEN 'failed' ELSE 'success' END AS status,
  '2000-01-01'::timestamptz
    + (n::double precision / :'rows_count'::double precision)
    * ('2000-02-01'::timestamptz - '2000-01-01'::timestamptz)
    + interval '2 seconds'
                                                        AS time,
  2                                                     AS attempt_number,
  false                                                 AS manual,
  CASE
    WHEN n % 20 != 0 THEN CASE WHEN n%2=0 THEN '200' ELSE '201' END
    ELSE                   CASE WHEN n%2=0 THEN '500' ELSE '422' END
  END                                                   AS code,
  NULL                                                  AS response_data,
  '2000-01-01'::timestamptz
    + (n::double precision / :'rows_count'::double precision)
    * ('2000-02-01'::timestamptz - '2000-01-01'::timestamptz)
                                                        AS event_time,
  (n % 3 != 2)                                          AS eligible_for_retry,
  '{}'                                                  AS event_data,
  '{}'::jsonb                                           AS event_metadata
FROM generate_series(0, :'rows_count'::int - 1) AS n
WHERE n % 5 = 0;

\echo [4/7] Inserting attempt 3 (5% of events)...

INSERT INTO attempts (
  id, event_id, tenant_id, destination_id, topic, status, time,
  attempt_number, manual, code, response_data,
  event_time, eligible_for_retry, event_data, event_metadata
)
SELECT
  'att_' || n || '_2'                                   AS id,
  'evt_' || n                                           AS event_id,
  CASE WHEN n % 10 = 0 THEN 'tenant_1' ELSE 'tenant_0' END
                                                        AS tenant_id,
  'dest_' || (n % 500)                                  AS destination_id,
  CASE n % 3
    WHEN 0 THEN 'order.created'
    WHEN 1 THEN 'order.updated'
    ELSE 'payment.received'
  END                                                   AS topic,
  CASE WHEN n % 100 = 0 THEN 'failed' ELSE 'success' END AS status,
  '2000-01-01'::timestamptz
    + (n::double precision / :'rows_count'::double precision)
    * ('2000-02-01'::timestamptz - '2000-01-01'::timestamptz)
    + interval '3 seconds'
                                                        AS time,
  3                                                     AS attempt_number,
  (n % 10 = 9)                                          AS manual,
  CASE
    WHEN n % 100 != 0 THEN CASE WHEN n%2=0 THEN '200' ELSE '201' END
    ELSE                    CASE WHEN n%2=0 THEN '500' ELSE '422' END
  END                                                   AS code,
  NULL                                                  AS response_data,
  '2000-01-01'::timestamptz
    + (n::double precision / :'rows_count'::double precision)
    * ('2000-02-01'::timestamptz - '2000-01-01'::timestamptz)
                                                        AS event_time,
  (n % 3 != 2)                                          AS eligible_for_retry,
  '{}'                                                  AS event_data,
  '{}'::jsonb                                           AS event_metadata
FROM generate_series(0, :'rows_count'::int - 1) AS n
WHERE n % 20 = 0;

\echo [5/7] Inserting attempt 4 (1% of events)...

INSERT INTO attempts (
  id, event_id, tenant_id, destination_id, topic, status, time,
  attempt_number, manual, code, response_data,
  event_time, eligible_for_retry, event_data, event_metadata
)
SELECT
  'att_' || n || '_3'                                   AS id,
  'evt_' || n                                           AS event_id,
  CASE WHEN n % 10 = 0 THEN 'tenant_1' ELSE 'tenant_0' END
                                                        AS tenant_id,
  'dest_' || (n % 500)                                  AS destination_id,
  CASE n % 3
    WHEN 0 THEN 'order.created'
    WHEN 1 THEN 'order.updated'
    ELSE 'payment.received'
  END                                                   AS topic,
  CASE WHEN n % 200 = 0 THEN 'failed' ELSE 'success' END AS status,
  '2000-01-01'::timestamptz
    + (n::double precision / :'rows_count'::double precision)
    * ('2000-02-01'::timestamptz - '2000-01-01'::timestamptz)
    + interval '4 seconds'
                                                        AS time,
  4                                                     AS attempt_number,
  (n % 10 = 9)                                          AS manual,
  CASE
    WHEN n % 200 != 0 THEN CASE WHEN n%2=0 THEN '200' ELSE '201' END
    ELSE                    CASE WHEN n%2=0 THEN '500' ELSE '422' END
  END                                                   AS code,
  NULL                                                  AS response_data,
  '2000-01-01'::timestamptz
    + (n::double precision / :'rows_count'::double precision)
    * ('2000-02-01'::timestamptz - '2000-01-01'::timestamptz)
                                                        AS event_time,
  (n % 3 != 2)                                          AS eligible_for_retry,
  '{}'                                                  AS event_data,
  '{}'::jsonb                                           AS event_metadata
FROM generate_series(0, :'rows_count'::int - 1) AS n
WHERE n % 100 = 0;

-- ============================================================================
-- 3. ANALYZE
-- ============================================================================

\echo [6/7] Analyzing...

ANALYZE events;
ANALYZE attempts;

-- ============================================================================
-- 4. Report
-- ============================================================================

\echo [7/7] Done. Row counts:

SELECT 'events'   AS table_name, count(*) AS row_count FROM events
UNION ALL
SELECT 'attempts' AS table_name, count(*) AS row_count FROM attempts;

SELECT attempt_number, status, count(*) AS cnt
FROM attempts GROUP BY attempt_number, status
ORDER BY attempt_number, status;
