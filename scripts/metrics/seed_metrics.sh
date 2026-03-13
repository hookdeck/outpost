#!/usr/bin/env bash
#
# Seed metrics data for local testing.
#
# Generates realistic event→attempt chains: each event gets a first attempt
# (attempt_number=1) and optionally 1-3 retries (attempt_number=2,3,4).
# Earlier attempts in a retry chain fail; the final attempt may succeed or fail.
#
# Usage:
#   ./scripts/seed_metrics.sh                # defaults
#   ./scripts/seed_metrics.sh --dry-run      # print SQL only
#   ./scripts/seed_metrics.sh --clean        # delete seed data only
#   SEED_EVENTS=200 ./scripts/seed_metrics.sh
#
# Tunables (env vars):
#   SEED_TENANT        - tenant ID                    (default: tenant_1)
#   SEED_EVENTS        - number of events             (default: 150)
#   SEED_DAYS          - spread over N days back      (default: 30, 0 = last hour)
#   SEED_DENSE_DAY     - day offset for spike, 0=today (default: 4)
#   SEED_DENSE_RATIO   - fraction of events on spike   (default: 0.4)
#   SEED_ERROR_RATE    - overall failure rate           (default: 0.35)
#   SEED_RETRY_FRAC    - fraction of events that retry (default: 0.4)
#   SEED_MAX_RETRIES   - max retries per event (1-3)   (default: 3)
#   SEED_MANUAL_RATE   - fraction of retries that are manual (default: 0.1)
#   SEED_DESTINATIONS  - comma-separated dest IDs (auto-detected from DB)
#   SEED_TOPICS        - comma-separated topics
#   SEED_CODES         - failure HTTP codes            (default: 500,422)
#   SEED_SUCCESS_CODES - success HTTP codes            (default: 200,201)
#   PG_CONTAINER       - docker container name         (default: outpost-deps-postgres-1)
#   PG_USER            - postgres user                 (default: outpost)
#   PG_DB              - postgres database             (default: outpost)
#
# IDs are prefixed with "seed_" for easy cleanup:
#   DELETE FROM attempts WHERE id LIKE 'seed_%';
#   DELETE FROM events WHERE id LIKE 'seed_%';

set -euo pipefail

# ── Tunables ──────────────────────────────────────────────────────────────────

TENANT="${SEED_TENANT:-tenant_1}"
EVENTS="${SEED_EVENTS:-10000}"
DAYS="${SEED_DAYS:-30}"
DENSE_DAY="${SEED_DENSE_DAY:-4}"
DENSE_RATIO="${SEED_DENSE_RATIO:-0.4}"
ERROR_RATE="${SEED_ERROR_RATE:-0.35}"
RETRY_FRAC="${SEED_RETRY_FRAC:-0.4}"
MAX_RETRIES="${SEED_MAX_RETRIES:-3}"
MANUAL_RATE="${SEED_MANUAL_RATE:-0.1}"
PG_CONTAINER="${PG_CONTAINER:-outpost-deps-postgres-1}"
PG_USER="${PG_USER:-outpost}"
PG_DB="${PG_DB:-outpost}"

IFS=',' read -ra TOPICS <<< "${SEED_TOPICS:-order.completed,payment.processed,user.signup}"
IFS=',' read -ra FAIL_CODES <<< "${SEED_CODES:-500,422}"
IFS=',' read -ra OK_CODES <<< "${SEED_SUCCESS_CODES:-200,201}"

DRY_RUN=false
CLEAN_ONLY=false
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=true
[[ "${1:-}" == "--clean" ]] && CLEAN_ONLY=true

# ── Clean helper ────────────────────────────────────────────────────────────

clean_seed_data() {
  echo "Cleaning existing seed data..."
  docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -q \
    -c "DELETE FROM attempts WHERE id LIKE 'seed_%'; DELETE FROM events WHERE id LIKE 'seed_%';"
  echo "Cleaned."
}

if $CLEAN_ONLY; then
  clean_seed_data
  exit 0
fi

if (( EVENTS == 0 )); then
  clean_seed_data
  exit 0
fi

# ── Auto-detect destinations ─────────────────────────────────────────────────

if [[ -z "${SEED_DESTINATIONS:-}" ]]; then
  DEST_RAW=$(docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -t -A \
    -c "SELECT DISTINCT destination_id FROM attempts WHERE tenant_id = '$TENANT' AND destination_id != '' AND id NOT LIKE 'seed_%' LIMIT 5;" 2>/dev/null || true)
  if [[ -z "$DEST_RAW" ]]; then
    DEST_RAW="des_test"
    echo "Warning: No destinations found for $TENANT, using fallback ID: des_test"
  fi
  IFS=$'\n' read -ra DESTINATIONS <<< "$DEST_RAW"
else
  IFS=',' read -ra DESTINATIONS <<< "$SEED_DESTINATIONS"
fi

NUM_TOPICS=${#TOPICS[@]}
NUM_DESTS=${#DESTINATIONS[@]}
NUM_FAIL_CODES=${#FAIL_CODES[@]}
NUM_OK_CODES=${#OK_CODES[@]}

# Pre-compute integer thresholds
error_thresh=$(awk "BEGIN {printf \"%d\", $ERROR_RATE * 100}")
retry_thresh=$(awk "BEGIN {printf \"%d\", $RETRY_FRAC * 100}")
manual_thresh=$(awk "BEGIN {printf \"%d\", $MANUAL_RATE * 100}")

echo "── Seed config ──"
echo "  tenant:       $TENANT"
echo "  events:       $EVENTS"
echo "  days back:    $DAYS"
echo "  dense day:    $DENSE_DAY days ago"
echo "  dense ratio:  $DENSE_RATIO"
echo "  error rate:   $ERROR_RATE"
echo "  retry frac:   $RETRY_FRAC"
echo "  max retries:  $MAX_RETRIES"
echo "  manual rate:  $MANUAL_RATE"
echo "  topics:       ${TOPICS[*]}"
echo "  destinations: ${DESTINATIONS[*]}"
echo "  fail codes:   ${FAIL_CODES[*]}"
echo "  ok codes:     ${OK_CODES[*]}"
echo ""

# ── Generate SQL ──────────────────────────────────────────────────────────────

generate_sql() {
  local now_epoch today_start
  now_epoch=$(date +%s)
  today_start=$(( now_epoch - (now_epoch % 86400) ))

  local dense_count sparse_count
  if (( DAYS == 0 )); then
    dense_count=0
    sparse_count=$EVENTS
  else
    dense_count=$(awk "BEGIN {printf \"%d\", $EVENTS * $DENSE_RATIO}")
    sparse_count=$(( EVENTS - dense_count ))
  fi

  local total_attempts=0
  local total_retries=0

  cat << 'HEADER'
-- Seed metrics data (auto-generated)
-- Clean up: DELETE FROM attempts WHERE id LIKE 'seed_%'; DELETE FROM events WHERE id LIKE 'seed_%';

BEGIN;
HEADER

  local atm_seq=0

  for (( i = 0; i < EVENTS; i++ )); do
    # ── Event time distribution ──
    local evt_epoch
    if (( DAYS == 0 )); then
      local minutes_ago=$(( (EVENTS - i) * 55 / EVENTS ))
      evt_epoch=$(( now_epoch - (minutes_ago * 60) + (i % 30) ))
    elif (( i < sparse_count )); then
      local day_offset=$(( (i * DAYS / sparse_count) ))
      [[ "$day_offset" -eq "$DENSE_DAY" ]] && day_offset=$(( day_offset + 1 ))
      local hour=$(( (i % 10) + 8 ))
      local minute=$(( (i * 7) % 60 ))
      evt_epoch=$(( today_start - (day_offset * 86400) + (hour * 3600) + (minute * 60) ))
    else
      local dense_i=$(( i - sparse_count ))
      local bucket
      local pct=$(( dense_i * 100 / dense_count ))
      if   (( pct < 10 )); then bucket=10
      elif (( pct < 30 )); then bucket=11
      elif (( pct < 70 )); then bucket=12
      elif (( pct < 90 )); then bucket=13
      else                      bucket=14
      fi
      local minute=$(( (dense_i * 13) % 60 ))
      local second=$(( (dense_i * 7) % 60 ))
      evt_epoch=$(( today_start - (DENSE_DAY * 86400) + (bucket * 3600) + (minute * 60) + second ))
    fi

    local evt_ts
    evt_ts=$(date -r "$evt_epoch" -u '+%Y-%m-%dT%H:%M:%S+00' 2>/dev/null || \
             date -d "@$evt_epoch" -u '+%Y-%m-%dT%H:%M:%S+00' 2>/dev/null)

    # ── Event dimensions ──
    local topic="${TOPICS[$(( i % NUM_TOPICS ))]}"
    local dest="${DESTINATIONS[$(( i % NUM_DESTS ))]}"
    local eligible="true"
    if (( i % 3 == 2 )); then eligible="false"; fi

    local evt_id="seed_evt_$(printf '%04d' $i)"

    # Event row
    echo "INSERT INTO events (id, tenant_id, destination_id, time, topic, eligible_for_retry, data, metadata)"
    echo "  VALUES ('$evt_id', '$TENANT', '$dest', '$evt_ts', '$topic', $eligible, '{\"seed\":true,\"index\":$i}', '{\"source\":\"seed\"}');"

    # ── Decide retry chain for this event ──
    # Does this event get retries?
    local retry_hash=$(( (i * 53 + 7) % 100 ))
    local num_retries=0
    if (( retry_hash < retry_thresh )); then
      # 1 to MAX_RETRIES retries
      num_retries=$(( (i % MAX_RETRIES) + 1 ))
    fi

    local total_chain=$(( num_retries + 1 ))  # first attempt + retries

    # ── Generate attempts for this event ──
    # Pattern: first attempt (attempt_number=1), then retries (2,3,...)
    # All attempts except the last one FAIL (that's why we retry).
    # The last attempt succeeds or fails based on error_rate.
    for (( a = 1; a <= total_chain; a++ )); do
      local atm_id="seed_atm_$(printf '%05d' $atm_seq)"
      atm_seq=$(( atm_seq + 1 ))

      # Attempt time: event_time + (attempt_number * 30-120 seconds)
      local delay=$(( (a * 60) + (atm_seq % 60) + 1 ))
      local atm_epoch=$(( evt_epoch + delay ))
      local atm_ts
      atm_ts=$(date -r "$atm_epoch" -u '+%Y-%m-%dT%H:%M:%S+00' 2>/dev/null || \
               date -d "@$atm_epoch" -u '+%Y-%m-%dT%H:%M:%S+00' 2>/dev/null)

      local status code manual

      # Every attempt independently uses SEED_ERROR_RATE
      local err_hash=$(( (i * 97 + a * 31 + 13) % 100 ))
      if (( err_hash < error_thresh )); then
        status="failed"
        code="${FAIL_CODES[$(( atm_seq % NUM_FAIL_CODES ))]}"
      else
        status="success"
        code="${OK_CODES[$(( atm_seq % NUM_OK_CODES ))]}"
      fi

      # Manual: only retries (attempt_number > 1) can be manual
      manual="false"
      if (( a > 1 )); then
        local manual_hash=$(( (atm_seq * 31 + 17) % 100 ))
        if (( manual_hash < manual_thresh )); then
          manual="true"
        fi
        total_retries=$(( total_retries + 1 ))
      fi

      total_attempts=$(( total_attempts + 1 ))

      echo "INSERT INTO attempts (id, event_id, destination_id, status, time, code, response_data, manual, attempt_number, tenant_id, topic, event_time, eligible_for_retry, event_data, event_metadata)"
      echo "  VALUES ('$atm_id', '$evt_id', '$dest', '$status', '$atm_ts', '$code', '{\"seed\":true}', $manual, $a, '$TENANT', '$topic', '$evt_ts', $eligible, '{\"seed\":true,\"index\":$i}', '{\"source\":\"seed\"}');"
    done
  done

  echo "COMMIT;"

  # Output stats to stderr so they don't end up in SQL
  echo "STATS:$EVENTS:$total_attempts:$total_retries" >&2
}

# ── Clean existing seed data first ────────────────────────────────────────────

clean_seed_data

# ── Generate ─────────────────────────────────────────────────────────────────

STATS=""
SQL=$(generate_sql 2> >(while read -r line; do
  if [[ "$line" == STATS:* ]]; then
    STATS="$line"
  else
    echo "$line" >&2
  fi
done; echo "$STATS" > /tmp/seed_metrics_stats))

# Read stats
if [[ -f /tmp/seed_metrics_stats ]]; then
  STATS=$(cat /tmp/seed_metrics_stats)
  rm -f /tmp/seed_metrics_stats
fi

IFS=':' read -r _ stat_events stat_attempts stat_retries <<< "$STATS"
stat_events="${stat_events:-$EVENTS}"
stat_attempts="${stat_attempts:-?}"
stat_retries="${stat_retries:-?}"

if $DRY_RUN; then
  echo "$SQL"
  echo ""
  echo "── Dry run complete. $stat_events events + $stat_attempts attempts ($stat_retries retries) would be inserted."
  exit 0
fi

# ── Execute ───────────────────────────────────────────────────────────────────

echo "Inserting $stat_events events + $stat_attempts attempts ($stat_retries retries)..."
echo "$SQL" | docker exec -i "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -q

# Verify
COUNTS=$(docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -t -A -c \
  "SELECT 'events=' || count(*) FROM events WHERE id LIKE 'seed_%'
   UNION ALL
   SELECT 'attempts=' || count(*) FROM attempts WHERE id LIKE 'seed_%'
   UNION ALL
   SELECT 'first_attempts=' || count(*) FROM attempts WHERE id LIKE 'seed_%' AND attempt_number = 1
   UNION ALL
   SELECT 'retries=' || count(*) FROM attempts WHERE id LIKE 'seed_%' AND attempt_number > 1;")

echo ""
echo "── Done ──"
echo "$COUNTS"
echo ""

# Quick distribution check
DIST=$(docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -t -A -c \
  "SELECT
     'success=' || count(*) FILTER (WHERE status = 'success') ||
     ' failed=' || count(*) FILTER (WHERE status = 'failed') ||
     ' error_rate=' || round(count(*) FILTER (WHERE status = 'failed')::numeric / NULLIF(count(*), 0), 3) ||
     ' avg_attempt=' || round(avg(attempt_number)::numeric, 2)
   FROM attempts WHERE id LIKE 'seed_%';")
echo "Distribution: $DIST"
echo ""
echo "To clean up:  docker exec $PG_CONTAINER psql -U $PG_USER -d $PG_DB -c \"DELETE FROM attempts WHERE id LIKE 'seed_%'; DELETE FROM events WHERE id LIKE 'seed_%';\""
