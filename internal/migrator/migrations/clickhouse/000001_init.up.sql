CREATE TABLE IF NOT EXISTS events (
  id String,
  tenant_id String,
  destination_id String,
  topic String,
  eligible_for_retry Bool,
  time DateTime,
  metadata String,
  data String
) ENGINE = MergeTree
ORDER BY
  (id, time);

CREATE TABLE IF NOT EXISTS deliveries (
  id String,
  delivery_event_id String,
  event_id String,
  destination_id String,
  status String,
  time DateTime
) ENGINE = ReplacingMergeTree
ORDER BY
  (id, time);