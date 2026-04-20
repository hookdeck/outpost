#!/bin/bash
set -uo pipefail

# End-to-end test for the loadtest app
# Prerequisites: Outpost running on :3333, loadtest app on :9090

BASE=http://localhost:9090
OUTPOST=http://localhost:3333
PASS=0
FAIL=0

pass() { echo "  ✓ $1"; PASS=$((PASS + 1)); }
fail() { echo "  ✗ $1: $2"; FAIL=$((FAIL + 1)); }

assert_status() {
  local expected=$1 actual=$2 msg=$3
  if [ "$actual" = "$expected" ]; then pass "$msg"; else fail "$msg" "expected $expected, got $actual"; fi
}

assert_contains() {
  local expected=$1 actual=$2 msg=$3
  if echo "$actual" | grep -q "$expected"; then pass "$msg"; else fail "$msg" "expected to contain '$expected', got: $actual"; fi
}

echo "=== Loadtest App E2E Tests ==="
echo ""

# Cleanup from previous runs
curl -s -X POST $BASE/api/groups/e2e-test/stop >/dev/null 2>&1 || true
curl -s -X DELETE $BASE/api/groups/e2e-test >/dev/null 2>&1 || true
curl -s -X DELETE $BASE/api/groups/g1 >/dev/null 2>&1 || true
curl -s -X DELETE $BASE/api/groups/g2 >/dev/null 2>&1 || true
# Also clean up Outpost tenants directly
curl -s -X DELETE $OUTPOST/api/v1/tenants/lt-e2e-0 -H "Authorization: Bearer apikey" >/dev/null 2>&1 || true
curl -s -X DELETE $OUTPOST/api/v1/tenants/lt-g1-0 -H "Authorization: Bearer apikey" >/dev/null 2>&1 || true
curl -s -X DELETE $OUTPOST/api/v1/tenants/lt-g2-0 -H "Authorization: Bearer apikey" >/dev/null 2>&1 || true

# ─────────────────────────────────────────────
echo "--- Test 1: Status endpoint ---"
RESP=$(curl -s $BASE/api/status)
assert_contains '"ok":true' "$RESP" "status returns ok"
assert_contains '"outpost_url"' "$RESP" "status has outpost_url"
assert_contains '"mock_url"' "$RESP" "status has mock_url"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 2: Webhook health ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' $BASE/webhook/health)
assert_status "200" "$STATUS" "webhook health returns 200"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 3: Dashboard serves HTML ---"
RESP=$(curl -s $BASE/)
assert_contains "Outpost Load Test" "$RESP" "dashboard serves HTML"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 4: Create group ---"
RESP=$(curl -s -X POST $BASE/api/groups \
  -H "Content-Type: application/json" \
  -d '{
    "name": "e2e-test",
    "tenant_prefix": "lt-e2e",
    "tenant_count": 1,
    "destinations_per_tenant": 1,
    "topics": ["user.created"],
    "publish": {"rate_per_tenant": 10, "pattern": "constant", "payload_bytes": 256},
    "mock_profile": {"latency_ms": 0, "latency_jitter_ms": 0, "error_rate": 0.0}
  }')
assert_contains '"state":"created"' "$RESP" "group created with state=created"
assert_contains '"name":"e2e-test"' "$RESP" "group has correct name"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 5: List groups ---"
RESP=$(curl -s $BASE/api/groups)
assert_contains '"e2e-test"' "$RESP" "list contains our group"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 6: Get single group ---"
RESP=$(curl -s $BASE/api/groups/e2e-test)
assert_contains '"state":"created"' "$RESP" "get returns created state"
assert_contains '"rate_per_tenant":10' "$RESP" "get returns correct rate"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 7: Cannot start without provisioning ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST $BASE/api/groups/e2e-test/start)
assert_status "409" "$STATUS" "start without provision returns 409"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 8: Provision group ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST $BASE/api/groups/e2e-test/provision)
assert_status "202" "$STATUS" "provision returns 202"

# Wait for provisioning to complete
echo "  (waiting for provisioning...)"
for i in $(seq 1 20); do
  sleep 1
  STATE=$(curl -s $BASE/api/groups/e2e-test | grep -o '"state":"[^"]*"' | head -1)
  if echo "$STATE" | grep -q "provisioned"; then
    break
  fi
  if echo "$STATE" | grep -q "error"; then
    fail "provision" "group entered error state"
    RESP=$(curl -s $BASE/api/groups/e2e-test)
    echo "    Group response: $RESP"
    break
  fi
done

RESP=$(curl -s $BASE/api/groups/e2e-test)
assert_contains '"state":"provisioned"' "$RESP" "group is provisioned"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 9: Verify tenant exists in Outpost ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer apikey" $OUTPOST/api/v1/tenants/lt-e2e-0)
assert_status "200" "$STATUS" "tenant lt-e2e-0 exists in Outpost"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 10: Start group ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST $BASE/api/groups/e2e-test/start)
assert_status "202" "$STATUS" "start returns 202"

RESP=$(curl -s $BASE/api/groups/e2e-test)
assert_contains '"state":"running"' "$RESP" "group is running"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 11: Wait for events and check metrics ---"
echo "  (waiting 8s for events to flow...)"
sleep 8

RESP=$(curl -s $BASE/api/groups/e2e-test)
PUBLISH_TOTAL=$(echo "$RESP" | grep -o '"publish_total":[0-9]*' | grep -o '[0-9]*')
DELIVERY_TOTAL=$(echo "$RESP" | grep -o '"delivery_total":[0-9]*' | grep -o '[0-9]*')

if [ "${PUBLISH_TOTAL:-0}" -gt 0 ]; then
  pass "publish_total > 0 (got $PUBLISH_TOTAL)"
else
  fail "publish_total" "expected > 0, got ${PUBLISH_TOTAL:-0}"
fi

if [ "${DELIVERY_TOTAL:-0}" -gt 0 ]; then
  pass "delivery_total > 0 (got $DELIVERY_TOTAL)"
else
  fail "delivery_total" "expected > 0, got ${DELIVERY_TOTAL:-0}. Events published but not delivered back?"
fi

# ─────────────────────────────────────────────
echo ""
echo "--- Test 12: Update rate mid-flight ---"
RESP=$(curl -s -X PATCH $BASE/api/groups/e2e-test \
  -H "Content-Type: application/json" \
  -d '{"rate_per_tenant": 50}')
assert_contains '"rate_per_tenant":50' "$RESP" "rate updated to 50"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 13: Update mock profile mid-flight ---"
RESP=$(curl -s -X PATCH $BASE/api/groups/e2e-test \
  -H "Content-Type: application/json" \
  -d '{"latency_ms": 100}')
assert_contains '"latency_ms":100' "$RESP" "mock latency updated to 100ms"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 14: Stop group ---"
RESP=$(curl -s -X POST $BASE/api/groups/e2e-test/stop)
assert_contains '"status":"stopped"' "$RESP" "stop returns stopped"

RESP=$(curl -s $BASE/api/groups/e2e-test)
assert_contains '"state":"stopped"' "$RESP" "group state is stopped"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 15: Metrics frozen after stop ---"
PUBLISH_AFTER_STOP=$(curl -s $BASE/api/groups/e2e-test | grep -o '"publish_total":[0-9]*' | grep -o '[0-9]*')
sleep 2
PUBLISH_LATER=$(curl -s $BASE/api/groups/e2e-test | grep -o '"publish_total":[0-9]*' | grep -o '[0-9]*')
if [ "$PUBLISH_AFTER_STOP" = "$PUBLISH_LATER" ]; then
  pass "metrics frozen after stop ($PUBLISH_LATER)"
else
  fail "metrics frozen" "publish_total changed from $PUBLISH_AFTER_STOP to $PUBLISH_LATER after stop"
fi

# ─────────────────────────────────────────────
echo ""
echo "--- Test 16: Reset group ---"
curl -s -X POST $BASE/api/groups/e2e-test/reset >/dev/null
RESP=$(curl -s $BASE/api/groups/e2e-test)
assert_contains '"state":"provisioned"' "$RESP" "reset returns to provisioned"
assert_contains '"publish_total":0' "$RESP" "metrics reset to 0"

# ─────────────────────────────────────────────
echo ""
echo "--- Test 17: Re-start after reset ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST $BASE/api/groups/e2e-test/start)
assert_status "202" "$STATUS" "re-start after reset works"
sleep 3
RESP=$(curl -s $BASE/api/groups/e2e-test)
PUBLISH_TOTAL=$(echo "$RESP" | grep -o '"publish_total":[0-9]*' | grep -o '[0-9]*')
if [ "${PUBLISH_TOTAL:-0}" -gt 0 ]; then
  pass "re-started and publishing again ($PUBLISH_TOTAL)"
else
  fail "re-start" "no publishes after re-start"
fi
curl -s -X POST $BASE/api/groups/e2e-test/stop >/dev/null

# ─────────────────────────────────────────────
echo ""
echo "--- Test 18: Teardown group ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST $BASE/api/groups/e2e-test/teardown)
assert_status "202" "$STATUS" "teardown returns 202"
sleep 2

# Verify tenant deleted from Outpost
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer apikey" $OUTPOST/api/v1/tenants/lt-e2e-0)
if [ "$STATUS" = "404" ] || [ "$STATUS" = "000" ]; then
  pass "tenant deleted from Outpost after teardown"
else
  fail "teardown" "tenant still exists in Outpost (status $STATUS)"
fi

# ─────────────────────────────────────────────
echo ""
echo "--- Test 19: Delete group ---"
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE $BASE/api/groups/e2e-test)
assert_status "204" "$STATUS" "delete returns 204"

RESP=$(curl -s $BASE/api/groups)
if [ "$RESP" = "[]" ] || echo "$RESP" | grep -q '^\[\]$'; then
  pass "no groups after delete"
else
  fail "no groups after delete" "got: $RESP"
fi

# ─────────────────────────────────────────────
echo ""
echo "--- Test 20: Global start/stop all ---"
# Create two groups
curl -s -X POST $BASE/api/groups -H "Content-Type: application/json" \
  -d '{"name":"g1","tenant_prefix":"lt-g1","tenant_count":1,"destinations_per_tenant":1,"topics":["test.event"],"publish":{"rate_per_tenant":5,"pattern":"constant","payload_bytes":128},"mock_profile":{"latency_ms":0,"latency_jitter_ms":0,"error_rate":0}}' >/dev/null
curl -s -X POST $BASE/api/groups -H "Content-Type: application/json" \
  -d '{"name":"g2","tenant_prefix":"lt-g2","tenant_count":1,"destinations_per_tenant":1,"topics":["test.event"],"publish":{"rate_per_tenant":5,"pattern":"constant","payload_bytes":128},"mock_profile":{"latency_ms":0,"latency_jitter_ms":0,"error_rate":0}}' >/dev/null

# Provision both
curl -s -X POST $BASE/api/groups/g1/provision >/dev/null
curl -s -X POST $BASE/api/groups/g2/provision >/dev/null

# Wait for both to be provisioned
for i in $(seq 1 15); do
  sleep 1
  S1=$(curl -s $BASE/api/groups/g1 | grep -o '"state":"[^"]*"' | head -1)
  S2=$(curl -s $BASE/api/groups/g2 | grep -o '"state":"[^"]*"' | head -1)
  if [ "$S1" = '"state":"provisioned"' ] && [ "$S2" = '"state":"provisioned"' ]; then
    break
  fi
done

# Start all
RESP=$(curl -s -X POST $BASE/api/start)
assert_contains '"started":2' "$RESP" "start all started 2 groups"

sleep 2

# Stop all
RESP=$(curl -s -X POST $BASE/api/stop)
assert_contains '"stopped":2' "$RESP" "stop all stopped 2 groups"

# Cleanup
curl -s -X POST $BASE/api/groups/g1/teardown >/dev/null
curl -s -X POST $BASE/api/groups/g2/teardown >/dev/null
sleep 1
curl -s -X DELETE $BASE/api/groups/g1 >/dev/null
curl -s -X DELETE $BASE/api/groups/g2 >/dev/null

pass "global start/stop cleanup complete"

# ─────────────────────────────────────────────
echo ""
echo "==================================="
echo "Results: $PASS passed, $FAIL failed"
echo "==================================="
[ $FAIL -eq 0 ] && exit 0 || exit 1
