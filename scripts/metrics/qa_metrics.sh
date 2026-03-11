#!/usr/bin/env bash
#
# QA test scenarios for the metrics dashboard.
#
# Usage:
#   ./scripts/qa_metrics.sh                 # list scenarios
#   ./scripts/qa_metrics.sh healthy         # run a scenario
#   ./scripts/qa_metrics.sh all             # run all scenarios interactively
#
# Each scenario cleans existing seed data, seeds fresh data with realistic
# event→attempt chains (retries share event_id), and prints a checklist.
#
# Dashboard layout (2 rows, 5 API calls):
#   Row 1: Event count (attempt_number=0)  |  Delivery events (stacked success/failed)
#   Row 2: Error rate  |  By status code (webhook only)  |  By topic
#
# Destinations list: 24h sparkline per row (attempt_number=0, 4h granularity)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SEED_SCRIPT="$SCRIPT_DIR/seed_metrics.sh"

if [[ ! -x "$SEED_SCRIPT" ]]; then
  chmod +x "$SEED_SCRIPT"
fi

# ── Scenario definitions ────────────────────────────────────────────────────

run_healthy() {
  echo "━━━ Scenario: HEALTHY DESTINATION ━━━"
  echo "Low error rate, few retries, steady traffic. The happy path."
  echo ""

  SEED_EVENTS=10000 SEED_DAYS=30 SEED_ERROR_RATE=0.05 SEED_RETRY_FRAC=0.1 \
    SEED_MAX_RETRIES=1 SEED_MANUAL_RATE=0.02 SEED_DENSE_RATIO=0.2 SEED_DENSE_DAY=5 \
    SEED_TOPICS="order.completed,payment.processed,user.signup" \
    "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
  Row 1:
    [ ] Event count: steady bars across 30d
    [ ] Delivery events: mostly green, tiny red slivers
    [ ] Event count ≈ delivery events (few retries)
  Row 2:
    [ ] Error rate: near 5%, spiky in 30d due to sparse buckets (try 7d for smoother line)
    [ ] By status code: 200/201 dominate, few 500/422
    [ ] By topic: 3 topics, all with low error rate
  Sparkline:
    [ ] Destinations list shows mini bar chart per destination
EOF
}

run_failing() {
  echo "━━━ Scenario: FAILING DESTINATION ━━━"
  echo "85% error rate, most events retry multiple times. Destination is down."
  echo ""

  SEED_EVENTS=750 SEED_DAYS=30 SEED_ERROR_RATE=0.85 SEED_RETRY_FRAC=0.8 \
    SEED_MAX_RETRIES=3 SEED_MANUAL_RATE=0.05 SEED_DENSE_RATIO=0.3 \
    SEED_TOPICS="order.completed,payment.processed" \
    "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
  Row 1:
    [ ] Event count: moderate bars
    [ ] Delivery events: MUCH taller than event count (retries inflate it)
    [ ] Delivery events: mostly red, some green
  Row 2:
    [ ] Error rate: line near 85%
    [ ] Error rate Y-axis: shows percentages (0%, 25%, 50%, 75%, 100%), NOT integers
    [ ] Error rate line: smooth, NOT zigzag between 0 and 1
    [ ] By status code: 500/422 dominate
    [ ] By topic: both topics ~85% error rate
  KEY CHECK:
    [ ] Delivery events count >> Event count (retries make more attempts)
EOF
}

run_spike() {
  echo "━━━ Scenario: INCIDENT SPIKE ━━━"
  echo "Volume spike 3 days ago with 60% error rate and heavy retries."
  echo ""

  SEED_EVENTS=1000 SEED_DAYS=30 SEED_DENSE_DAY=3 SEED_DENSE_RATIO=0.5 \
    SEED_ERROR_RATE=0.6 SEED_RETRY_FRAC=0.7 SEED_MAX_RETRIES=3 \
    SEED_MANUAL_RATE=0.15 \
    SEED_TOPICS="order.completed,payment.processed,user.signup,invoice.created" \
    "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
  30d view:
    [ ] Event count: visible volume spike 3 days ago
    [ ] Delivery events: spike even larger (retries amplify it)
    [ ] Error rate: ~60% overall (smoother near spike due to more data, noisy elsewhere)
  7d view:
    [ ] Volume spike more prominent
  Breakdown:
    [ ] By status code: 500/422 significant
    [ ] By topic: 4 topics visible, all ~60% error rate
EOF
}

run_empty() {
  echo "━━━ Scenario: EMPTY STATE ━━━"
  echo "No data — verify empty state rendering."
  echo ""

  SEED_EVENTS=0 "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
    [ ] All charts show "No data" empty state (gated by hasActivity)
    [ ] No JS errors in browser console
    [ ] Timeframe buttons still work (switching doesn't crash)
    [ ] Loading spinners appear briefly on switch
EOF
}

run_single() {
  echo "━━━ Scenario: SINGLE EVENT ━━━"
  echo "1 event, 1 attempt — minimal data edge case."
  echo ""

  SEED_EVENTS=1 SEED_DAYS=1 SEED_ERROR_RATE=0 SEED_RETRY_FRAC=0 \
    SEED_MANUAL_RATE=0 SEED_DENSE_RATIO=0 \
    SEED_TOPICS="order.completed" \
    "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
  24h view:
    [ ] Event count: single bar, count=1
    [ ] Delivery events: single green bar, count=1
    [ ] Event count = delivery events (no retries)
    [ ] Error rate: 0%
    [ ] By status code: single row (200 or 201), count=1 (webhook only)
    [ ] By topic: single row (order.completed), 0% error
  Edge cases:
    [ ] Charts don't look broken with 1 point
    [ ] Tooltips work on hover
EOF
}

run_all_fail() {
  echo "━━━ Scenario: 100% ERROR RATE ━━━"
  echo "Every attempt fails, many retries — worst case."
  echo ""

  SEED_EVENTS=500 SEED_DAYS=7 SEED_ERROR_RATE=1.0 SEED_RETRY_FRAC=0.7 \
    SEED_MAX_RETRIES=3 SEED_MANUAL_RATE=0 \
    SEED_TOPICS="webhook.delivery" \
    "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
  7d view:
    [ ] Event count: moderate bars
    [ ] Delivery events: ALL red, zero green — much taller than event count
    [ ] Error rate: flat line at 100%
    [ ] Error rate Y-axis: 100% at top
    [ ] By status code: only 500/422, no 200/201
    [ ] By topic: "webhook.delivery" with 100.0% error
  Critical:
    [ ] successful_count = 0 everywhere
    [ ] No division-by-zero errors
EOF
}

run_all_success() {
  echo "━━━ Scenario: 0% ERROR RATE ━━━"
  echo "Every attempt succeeds, no retries."
  echo ""

  SEED_EVENTS=500 SEED_DAYS=7 SEED_ERROR_RATE=0 SEED_RETRY_FRAC=0 \
    SEED_MANUAL_RATE=0 SEED_DENSE_RATIO=0.3 \
    SEED_TOPICS="order.completed,user.signup" \
    "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
  7d view:
    [ ] Event count = delivery events (no retries at all)
    [ ] Delivery events: ALL green, zero red
    [ ] Error rate: flat line at 0%
    [ ] By status code: only 200/201
    [ ] By topic: 2 topics, both 0.0% error
  Critical:
    [ ] failed_count = 0 everywhere
    [ ] Error rate chart renders at 0 (not blank)
EOF
}

run_recent() {
  echo "━━━ Scenario: RECENT DATA (1h view) ━━━"
  echo "60 events in the last ~55 minutes — test 1m granularity."
  echo ""

  SEED_EVENTS=60 SEED_DAYS=0 \
    SEED_ERROR_RATE=0.3 SEED_RETRY_FRAC=0.4 SEED_MAX_RETRIES=2 \
    SEED_MANUAL_RATE=0.1 \
    SEED_TOPICS="order.completed,user.signup" \
    "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
  1h view:
    [ ] Data visible with per-minute granularity
    [ ] X-axis shows HH:MM format
    [ ] Event count: ~60 bars spread across the hour
    [ ] Delivery events: taller due to retries
  24h view:
    [ ] Data clustered in the most recent hour
  7d / 30d views:
    [ ] All data on today's bar
EOF
}

run_many_topics() {
  echo "━━━ Scenario: MANY TOPICS ━━━"
  echo "10 different topics — test breakdown table layout."
  echo ""

  SEED_EVENTS=1000 SEED_DAYS=30 SEED_ERROR_RATE=0.3 SEED_RETRY_FRAC=0.3 \
    SEED_MAX_RETRIES=2 SEED_DENSE_RATIO=0.3 \
    SEED_TOPICS="order.completed,order.refunded,order.cancelled,payment.processed,payment.failed,user.signup,user.updated,user.deleted,invoice.created,invoice.paid" \
    "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
  30d view:
    [ ] By topic: 10 rows visible
    [ ] Sorted descending by count
    [ ] Counts roughly equal (~130 each, includes retries)
    [ ] Error rates shown per topic
    [ ] Table doesn't overflow or break layout
    [ ] Bar widths proportional to count
EOF
}

run_many_codes() {
  echo "━━━ Scenario: MANY STATUS CODES ━━━"
  echo "Varied HTTP codes — test status code breakdown."
  echo ""

  SEED_EVENTS=1000 SEED_DAYS=30 SEED_ERROR_RATE=0.5 SEED_RETRY_FRAC=0.5 \
    SEED_MAX_RETRIES=2 SEED_DENSE_RATIO=0.3 \
    SEED_CODES="500,502,503,422,400,403,429" \
    SEED_SUCCESS_CODES="200,201" \
    SEED_TOPICS="order.completed,payment.processed,user.signup" \
    "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
  30d view:
    [ ] By status code: 9 codes visible (200,201,400,403,422,429,500,502,503)
    [ ] 2xx bars are green, 4xx/5xx bars are red
    [ ] Sorted descending by count
    [ ] Bar widths proportional
    [ ] Error rate ~50%
EOF
}

run_retry_heavy() {
  echo "━━━ Scenario: RETRY HEAVY ━━━"
  echo "Most events retry 2-3 times. Tests event_count vs delivery_events gap."
  echo ""

  SEED_EVENTS=500 SEED_DAYS=7 SEED_ERROR_RATE=0.4 SEED_RETRY_FRAC=0.9 \
    SEED_MAX_RETRIES=3 SEED_MANUAL_RATE=0.2 SEED_DENSE_RATIO=0.3 \
    SEED_TOPICS="order.completed,payment.processed" \
    "$SEED_SCRIPT"

  cat << 'EOF'

── Verify ──
  7d view:
    [ ] Event count bars are SHORTER than delivery events bars
    [ ] Ratio: delivery_events should be ~2-3x event_count
  KEY CHECK:
    This scenario exists to verify that the event_count (attempt_number=0)
    chart shows fewer items than the delivery events (all attempts) chart.
    If they look identical, the retry chains are not working correctly.
EOF
}

# ── Main ─────────────────────────────────────────────────────────────────────

print_usage() {
  cat << 'EOF'
Metrics Dashboard QA Scenarios:

  healthy       Low error rate, steady traffic (baseline)
  failing       85% error rate, destination is down
  spike         Incident spike 3 days ago
  empty         No data (empty state)
  single        Single event, single attempt
  all-fail      100% error rate, many retries
  all-success   0% error rate, no retries
  recent        Last hour only (1m granularity test)
  many-topics   10 topics (breakdown table test)
  many-codes    9 HTTP status codes (code breakdown test)
  retry-heavy   90% events retry 2-3x (event_count vs delivery gap test)
  all           Run all scenarios interactively

Usage: ./scripts/qa_metrics.sh <scenario>
EOF
}

run_all() {
  local scenarios=(healthy failing spike empty single all-fail all-success recent many-topics many-codes retry-heavy)
  for scenario in "${scenarios[@]}"; do
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║  Next scenario: $scenario"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""
    read -rp "Press Enter to run '$scenario' (or 'q' to quit): " input
    [[ "$input" == "q" ]] && break

    case "$scenario" in
      healthy)      run_healthy ;;
      failing)      run_failing ;;
      spike)        run_spike ;;
      empty)        run_empty ;;
      single)       run_single ;;
      all-fail)     run_all_fail ;;
      all-success)  run_all_success ;;
      recent)       run_recent ;;
      many-topics)  run_many_topics ;;
      many-codes)   run_many_codes ;;
      retry-heavy)  run_retry_heavy ;;
    esac

    echo ""
    echo "────────────────────────────────────────────────────────────────"
    echo "  QA the portal now. When ready, press Enter for next scenario."
    echo "────────────────────────────────────────────────────────────────"
    read -rp ""
  done
  echo "Done!"
}

case "${1:-}" in
  healthy)      run_healthy ;;
  failing)      run_failing ;;
  spike)        run_spike ;;
  empty)        run_empty ;;
  single)       run_single ;;
  all-fail)     run_all_fail ;;
  all-success)  run_all_success ;;
  recent)       run_recent ;;
  many-topics)  run_many_topics ;;
  many-codes)   run_many_codes ;;
  retry-heavy)  run_retry_heavy ;;
  all)          run_all ;;
  *)            print_usage ;;
esac
