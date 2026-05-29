#!/usr/bin/env bash
# End-to-end smoke test: provisions a tenant + webhook destination, publishes
# one event to mock.hookdeck.com, and polls until the delivery attempt is
# persisted. Cleans up on exit.
#
# Validates the full publish → delivery → log pipeline transitively:
# api, redis, mq, delivery worker, egress, log worker, log store.
set -uo pipefail

API="${OUTPOST_API:-http://localhost:3333/api/v1}"
KEY="${OUTPOST_API_KEY:-apikey}"
WEBHOOK="${OUTPOST_SMOKE_WEBHOOK:-https://mock.hookdeck.com}"
TOPIC="${OUTPOST_SMOKE_TOPIC:-user.created}"
TIMEOUT_S="${OUTPOST_SMOKE_TIMEOUT:-10}"

# Unique-per-run ids so re-runs don't share state with each other.
SUFFIX="$(date +%s)$$"
TENANT="smoke_${SUFFIX}"
DEST="smoke_${SUFFIX}"
TRACE="smoke-${SUFFIX}"

if ! command -v jq >/dev/null 2>&1; then
  echo "smoke.sh requires jq" >&2
  exit 2
fi

step() { printf "  → %s\n" "$1"; }
ok()   { printf "  \033[32m✓\033[0m %s\n" "$1"; }
fail() { printf "  \033[31m✗\033[0m %s\n" "$1" >&2; exit 1; }

cleaned=0
cleanup() {
  [ "$cleaned" = "1" ] && return
  cleaned=1
  curl -fsS -X DELETE -H "Authorization: Bearer $KEY" \
    "$API/tenants/$TENANT/destinations/$DEST" >/dev/null 2>&1 || true
  curl -fsS -X DELETE -H "Authorization: Bearer $KEY" \
    "$API/tenants/$TENANT" >/dev/null 2>&1 || true
}
trap cleanup EXIT

req() {
  local method=$1 path=$2 body=${3:-}
  if [ -n "$body" ]; then
    curl -fsS -X "$method" \
      -H "Authorization: Bearer $KEY" \
      -H "Content-Type: application/json" \
      -d "$body" \
      "$API$path"
  else
    curl -fsS -X "$method" \
      -H "Authorization: Bearer $KEY" \
      "$API$path"
  fi
}

start=$(date +%s)

# 1. Tenant
step "PUT /tenants/$TENANT"
req PUT "/tenants/$TENANT" >/dev/null || fail "tenant upsert failed (api unreachable or rejecting auth)"
ok "tenant ready"

# 2. Destination (webhook)
step "POST /tenants/$TENANT/destinations"
dest_body=$(jq -nc \
  --arg id "$DEST" \
  --arg url "$WEBHOOK" \
  --arg topic "$TOPIC" \
  '{id:$id, type:"webhook", topics:[$topic], config:{url:$url}}')
dest_resp=$(req POST "/tenants/$TENANT/destinations" "$dest_body") \
  || fail "destination create failed"
dest_id=$(echo "$dest_resp" | jq -r '.id')
[ -n "$dest_id" ] && [ "$dest_id" != "null" ] || fail "destination create returned no id: $dest_resp"
ok "destination ready ($dest_id → $WEBHOOK)"

# 3. Publish
step "POST /publish (topic=$TOPIC, trace=$TRACE)"
event_body=$(jq -nc \
  --arg tenant "$TENANT" \
  --arg topic "$TOPIC" \
  --arg trace "$TRACE" \
  '{tenant_id:$tenant, topic:$topic, data:{trace:$trace, smoke:true}}')
pub_resp=$(req POST "/publish" "$event_body") \
  || fail "publish failed (rabbitmq or publishmq config likely wrong)"
event_id=$(echo "$pub_resp" | jq -r '.id // empty')
ok "event ingested${event_id:+ (id=$event_id)}"

# 4. Poll attempts until at least one successful delivery is logged.
step "polling attempts (up to ${TIMEOUT_S}s) — exercises mq → delivery → log → store"
deadline=$(( $(date +%s) + TIMEOUT_S ))
attempt_status=""
while [ "$(date +%s)" -lt "$deadline" ]; do
  attempts=$(req GET "/attempts?limit=20" 2>/dev/null || echo '{}')
  attempt_status=$(echo "$attempts" | jq -r --arg eid "$event_id" \
    '.models[] | select(.event_id == $eid) | .status' 2>/dev/null | head -1)
  if [ -n "$attempt_status" ]; then
    case "$attempt_status" in
      success|delivered) break ;;
    esac
  fi
  sleep 1
done

elapsed=$(( $(date +%s) - start ))

if [ -z "$attempt_status" ]; then
  fail "no delivery attempt logged after ${TIMEOUT_S}s — delivery worker or log worker likely stuck"
fi
case "$attempt_status" in
  success|delivered)
    ok "delivery attempt logged as $attempt_status"
    echo
    printf "\033[32mPipeline OK in %ss.\033[0m\n" "$elapsed"
    ;;
  *)
    fail "delivery attempt logged but status=$attempt_status (expected success)"
    ;;
esac
