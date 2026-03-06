-- seed.sql — Deterministic bulk seeding for CH metrics benchmarks.
--
-- Usage:
--   clickhouse client --port 9009 --database bench --param_rows 1000000000 < cmd/bench/metrics/ch/seed.sql
--
-- Default rows = 1000000000 (1B). Override with --param_rows N.
--
-- Distribution (same as PG bench):
--   2 tenants: tenant_0 gets 90%, tenant_1 gets 10%
--   Time: evenly spread across January 2000 (2000-01-01 to 2000-02-01)
--
-- Attempt chain (1 event -> 1-4 attempts):
--   attempt 0: all events.       Failed if n%5=0   (20%)
--   attempt 1: failed attempt 0. Failed if n%20=0  (25% of retries)
--   attempt 2: failed attempt 1. Failed if n%100=0 (20% of retries)
--   attempt 3: failed attempt 2. Failed if n%200=0 (50% of retries)
--
--   For 1B events -> ~1.26B attempts. 0.5% events permanently failed.

SELECT concat('Seeding ', toString({rows:UInt64}), ' events + chained attempts...') AS message;

-- ============================================================================
-- 1. Bulk INSERT into events
-- ============================================================================
--
-- Tenants:   n%10 == 0 -> tenant_1 (10%), else tenant_0 (90%)
-- Destinations: dest_(n%500)                   [500 destinations]
-- Topics:    n%3 -> order.created / order.updated / payment.received
-- Time:      Even spread across 2000-01-01 to 2000-02-01
-- eligible_for_retry: n%3 != 2

SELECT '[1/7] Inserting events...' AS message;

INSERT INTO events (event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data)
SELECT
  concat('evt_', toString(number))                              AS event_id,
  if(number % 10 = 0, 'tenant_1', 'tenant_0')                  AS tenant_id,
  concat('dest_', toString(number % 500))                       AS destination_id,
  multiIf(
    number % 3 = 0, 'order.created',
    number % 3 = 1, 'order.updated',
    'payment.received'
  )                                                             AS topic,
  number % 3 != 2                                               AS eligible_for_retry,
  toDateTime64('2000-01-01', 3)
    + toIntervalMillisecond(
        toUInt64(number * 2678400000 / {rows:UInt64})
      )                                                         AS event_time,
  '{}'                                                          AS metadata,
  '{}'                                                          AS data
FROM numbers({rows:UInt64});

-- ============================================================================
-- 2. Bulk INSERT into attempts (chained retries)
-- ============================================================================
--
-- Each attempt's time = event_time + (attempt_number * 1 second).
-- manual: only attempt_number >= 2 AND n%10=9 (10% of late retries).
-- Code: success->200/201, failed->500/422 (alternating on n%2).

SELECT '[2/7] Inserting attempt 0 (all events)...' AS message;

INSERT INTO attempts (
  event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data,
  attempt_id, status, attempt_time, code, response_data, manual, attempt_number
)
SELECT
  concat('evt_', toString(number))                              AS event_id,
  if(number % 10 = 0, 'tenant_1', 'tenant_0')                  AS tenant_id,
  concat('dest_', toString(number % 500))                       AS destination_id,
  multiIf(
    number % 3 = 0, 'order.created',
    number % 3 = 1, 'order.updated',
    'payment.received'
  )                                                             AS topic,
  number % 3 != 2                                               AS eligible_for_retry,
  toDateTime64('2000-01-01', 3)
    + toIntervalMillisecond(
        toUInt64(number * 2678400000 / {rows:UInt64})
      )                                                         AS event_time,
  '{}'                                                          AS metadata,
  '{}'                                                          AS data,
  concat('att_', toString(number), '_0')                        AS attempt_id,
  if(number % 5 = 0, 'failed', 'success')                      AS status,
  toDateTime64('2000-01-01', 3)
    + toIntervalMillisecond(
        toUInt64(number * 2678400000 / {rows:UInt64})
      )
    + toIntervalSecond(1)                                       AS attempt_time,
  multiIf(
    number % 5 != 0 AND number % 2 = 0, '200',
    number % 5 != 0, '201',
    number % 2 = 0, '500',
    '422'
  )                                                             AS code,
  ''                                                            AS response_data,
  false                                                         AS manual,
  toUInt32(0)                                                   AS attempt_number
FROM numbers({rows:UInt64});

SELECT '[3/7] Inserting attempt 1 (20% of events)...' AS message;

INSERT INTO attempts (
  event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data,
  attempt_id, status, attempt_time, code, response_data, manual, attempt_number
)
SELECT
  concat('evt_', toString(number))                              AS event_id,
  if(number % 10 = 0, 'tenant_1', 'tenant_0')                  AS tenant_id,
  concat('dest_', toString(number % 500))                       AS destination_id,
  multiIf(
    number % 3 = 0, 'order.created',
    number % 3 = 1, 'order.updated',
    'payment.received'
  )                                                             AS topic,
  number % 3 != 2                                               AS eligible_for_retry,
  toDateTime64('2000-01-01', 3)
    + toIntervalMillisecond(
        toUInt64(number * 2678400000 / {rows:UInt64})
      )                                                         AS event_time,
  '{}'                                                          AS metadata,
  '{}'                                                          AS data,
  concat('att_', toString(number), '_1')                        AS attempt_id,
  if(number % 20 = 0, 'failed', 'success')                     AS status,
  toDateTime64('2000-01-01', 3)
    + toIntervalMillisecond(
        toUInt64(number * 2678400000 / {rows:UInt64})
      )
    + toIntervalSecond(2)                                       AS attempt_time,
  multiIf(
    number % 20 != 0 AND number % 2 = 0, '200',
    number % 20 != 0, '201',
    number % 2 = 0, '500',
    '422'
  )                                                             AS code,
  ''                                                            AS response_data,
  false                                                         AS manual,
  toUInt32(1)                                                   AS attempt_number
FROM numbers({rows:UInt64})
WHERE number % 5 = 0;

SELECT '[4/7] Inserting attempt 2 (5% of events)...' AS message;

INSERT INTO attempts (
  event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data,
  attempt_id, status, attempt_time, code, response_data, manual, attempt_number
)
SELECT
  concat('evt_', toString(number))                              AS event_id,
  if(number % 10 = 0, 'tenant_1', 'tenant_0')                  AS tenant_id,
  concat('dest_', toString(number % 500))                       AS destination_id,
  multiIf(
    number % 3 = 0, 'order.created',
    number % 3 = 1, 'order.updated',
    'payment.received'
  )                                                             AS topic,
  number % 3 != 2                                               AS eligible_for_retry,
  toDateTime64('2000-01-01', 3)
    + toIntervalMillisecond(
        toUInt64(number * 2678400000 / {rows:UInt64})
      )                                                         AS event_time,
  '{}'                                                          AS metadata,
  '{}'                                                          AS data,
  concat('att_', toString(number), '_2')                        AS attempt_id,
  if(number % 100 = 0, 'failed', 'success')                    AS status,
  toDateTime64('2000-01-01', 3)
    + toIntervalMillisecond(
        toUInt64(number * 2678400000 / {rows:UInt64})
      )
    + toIntervalSecond(3)                                       AS attempt_time,
  multiIf(
    number % 100 != 0 AND number % 2 = 0, '200',
    number % 100 != 0, '201',
    number % 2 = 0, '500',
    '422'
  )                                                             AS code,
  ''                                                            AS response_data,
  number % 10 = 9                                               AS manual,
  toUInt32(2)                                                   AS attempt_number
FROM numbers({rows:UInt64})
WHERE number % 20 = 0;

SELECT '[5/7] Inserting attempt 3 (1% of events)...' AS message;

INSERT INTO attempts (
  event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data,
  attempt_id, status, attempt_time, code, response_data, manual, attempt_number
)
SELECT
  concat('evt_', toString(number))                              AS event_id,
  if(number % 10 = 0, 'tenant_1', 'tenant_0')                  AS tenant_id,
  concat('dest_', toString(number % 500))                       AS destination_id,
  multiIf(
    number % 3 = 0, 'order.created',
    number % 3 = 1, 'order.updated',
    'payment.received'
  )                                                             AS topic,
  number % 3 != 2                                               AS eligible_for_retry,
  toDateTime64('2000-01-01', 3)
    + toIntervalMillisecond(
        toUInt64(number * 2678400000 / {rows:UInt64})
      )                                                         AS event_time,
  '{}'                                                          AS metadata,
  '{}'                                                          AS data,
  concat('att_', toString(number), '_3')                        AS attempt_id,
  if(number % 200 = 0, 'failed', 'success')                    AS status,
  toDateTime64('2000-01-01', 3)
    + toIntervalMillisecond(
        toUInt64(number * 2678400000 / {rows:UInt64})
      )
    + toIntervalSecond(4)                                       AS attempt_time,
  multiIf(
    number % 200 != 0 AND number % 2 = 0, '200',
    number % 200 != 0, '201',
    number % 2 = 0, '500',
    '422'
  )                                                             AS code,
  ''                                                            AS response_data,
  number % 10 = 9                                               AS manual,
  toUInt32(3)                                                   AS attempt_number
FROM numbers({rows:UInt64})
WHERE number % 100 = 0;

-- ============================================================================
-- 3. OPTIMIZE (force ReplacingMergeTree merge)
-- ============================================================================

SELECT '[6/7] Optimizing (forcing merge)...' AS message;

OPTIMIZE TABLE events FINAL;
OPTIMIZE TABLE attempts FINAL;

-- ============================================================================
-- 4. Report
-- ============================================================================

SELECT '[7/7] Done. Row counts:' AS message;

SELECT 'events' AS table_name, count() AS row_count FROM events
UNION ALL
SELECT 'attempts' AS table_name, count() AS row_count FROM attempts;

SELECT attempt_number, status, count() AS cnt
FROM attempts GROUP BY attempt_number, status
ORDER BY attempt_number, status;
