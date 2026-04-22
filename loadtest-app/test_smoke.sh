#!/bin/bash
# Smoke test: full lifecycle via API + Playwright screenshots of dashboard
set -uo pipefail

BASE=http://localhost:9090
OUTPOST=http://localhost:3333
QA_DIR="$(cd "$(dirname "$0")/../../qa" && pwd)"
RESULTS_DIR="$(cd "$(dirname "$0")" && pwd)/test-results"
PASS=0
FAIL=0

pass() { echo "  ✓ $1"; PASS=$((PASS + 1)); }
fail() { echo "  ✗ $1: $2"; FAIL=$((FAIL + 1)); }

screenshot() {
  local name=$1
  node "$QA_DIR/browser.js" \
    --steps <(echo "[{\"action\":\"navigate\",\"url\":\"$BASE\"},{\"action\":\"sleep\",\"ms\":2000},{\"action\":\"screenshot\",\"name\":\"$name\"}]") \
    --output "$RESULTS_DIR" \
    --headless \
    --timeout 10000 >/dev/null 2>&1
  echo "  📸 $name"
}

mkdir -p "$RESULTS_DIR"
echo "=== Loadtest Smoke Test ==="
echo ""

# Cleanup
curl -s -X POST $BASE/api/groups/smoke/stop >/dev/null 2>&1
curl -s -X DELETE $BASE/api/groups/smoke >/dev/null 2>&1
curl -s -X DELETE $OUTPOST/api/v1/tenants/lt-smoke-0 -H "Authorization: Bearer apikey" >/dev/null 2>&1

# 1. Dashboard loads
echo "--- Dashboard ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' $BASE/)
if [ "$STATUS" = "200" ]; then pass "dashboard serves"; else fail "dashboard" "status $STATUS"; fi
screenshot "01-empty.png"

# 2. Create group
echo ""
echo "--- Create + Provision + Start ---"
RESP=$(curl -s -X POST $BASE/api/groups \
  -H "Content-Type: application/json" \
  -d '{
    "name": "smoke",
    "tenant_prefix": "lt-smoke",
    "tenant_count": 1,
    "destinations_per_tenant": 1,
    "topics": ["user.created"],
    "publish": {"rate_per_tenant": 20, "pattern": "constant", "payload_bytes": 256},
    "mock_profile": {"latency_ms": 50, "latency_jitter_ms": 10, "error_rate": 0.0}
  }')
if echo "$RESP" | grep -q '"state":"created"'; then pass "group created"; else fail "create" "$RESP"; fi
screenshot "02-created.png"

# 3. Provision
curl -s -X POST $BASE/api/groups/smoke/provision >/dev/null
echo "  (provisioning...)"
for i in $(seq 1 20); do
  sleep 1
  STATE=$(curl -s $BASE/api/groups/smoke | python3 -c "import sys,json; print(json.load(sys.stdin)['state'])" 2>/dev/null)
  if [ "$STATE" = "provisioned" ]; then break; fi
  if [ "$STATE" = "error" ]; then
    ERR=$(curl -s $BASE/api/groups/smoke | python3 -c "import sys,json; print(json.load(sys.stdin)['error'])" 2>/dev/null)
    fail "provision" "$ERR"
    break
  fi
done
if [ "$STATE" = "provisioned" ]; then pass "provisioned"; fi

# Verify in Outpost
OSTATUS=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer apikey" $OUTPOST/api/v1/tenants/lt-smoke-0)
if [ "$OSTATUS" = "200" ]; then pass "tenant exists in Outpost"; else fail "tenant" "status $OSTATUS"; fi
screenshot "03-provisioned.png"

# 4. Start
curl -s -X POST $BASE/api/groups/smoke/start >/dev/null
STATE=$(curl -s $BASE/api/groups/smoke | python3 -c "import sys,json; print(json.load(sys.stdin)['state'])" 2>/dev/null)
if [ "$STATE" = "running" ]; then pass "started"; else fail "start" "state=$STATE"; fi

# 5. Wait for events
echo ""
echo "--- Events flowing ---"
echo "  (waiting 10s for events...)"
sleep 10

RESP=$(curl -s $BASE/api/groups/smoke)
PUB=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['metrics']['publish_total'])" 2>/dev/null)
DEL=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['metrics']['delivery_total'])" 2>/dev/null)
P50=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['metrics']['e2e_latency_p50_ms'])" 2>/dev/null)
P95=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['metrics']['e2e_latency_p95_ms'])" 2>/dev/null)
ERR=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['metrics']['publish_errors'])" 2>/dev/null)

if [ "${PUB:-0}" -gt 0 ]; then pass "published $PUB events"; else fail "publish" "0 events"; fi
if [ "${DEL:-0}" -gt 0 ]; then pass "delivered $DEL events"; else fail "delivery" "0 events"; fi
echo "  ℹ p50=${P50}ms p95=${P95}ms errors=${ERR}"
screenshot "04-running.png"

# 6. Stop
echo ""
echo "--- Stop + Verify ---"
curl -s -X POST $BASE/api/groups/smoke/stop >/dev/null
STATE=$(curl -s $BASE/api/groups/smoke | python3 -c "import sys,json; print(json.load(sys.stdin)['state'])" 2>/dev/null)
if [ "$STATE" = "stopped" ]; then pass "stopped"; else fail "stop" "state=$STATE"; fi

PUB_AFTER=$(curl -s $BASE/api/groups/smoke | python3 -c "import sys,json; print(json.load(sys.stdin)['metrics']['publish_total'])" 2>/dev/null)
sleep 2
PUB_LATER=$(curl -s $BASE/api/groups/smoke | python3 -c "import sys,json; print(json.load(sys.stdin)['metrics']['publish_total'])" 2>/dev/null)
if [ "$PUB_AFTER" = "$PUB_LATER" ]; then pass "metrics frozen ($PUB_LATER)"; else fail "frozen" "$PUB_AFTER → $PUB_LATER"; fi
screenshot "05-stopped.png"

# 7. Teardown + Delete
echo ""
echo "--- Teardown ---"
curl -s -X POST $BASE/api/groups/smoke/teardown >/dev/null
sleep 3
curl -s -X DELETE $BASE/api/groups/smoke >/dev/null
sleep 1
COUNT=$(curl -s $BASE/api/groups | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null)
if [ "${COUNT:-1}" = "0" ]; then pass "cleaned up"; else fail "cleanup" "groups remaining: $COUNT"; fi
screenshot "06-clean.png"

echo ""
echo "==================================="
echo "Results: $PASS passed, $FAIL failed"
echo "Screenshots in $RESULTS_DIR"
echo "==================================="
[ $FAIL -eq 0 ] && exit 0 || exit 1
