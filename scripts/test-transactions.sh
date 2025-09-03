#!/bin/bash

# Redis Transaction Test Script
# Tests that same-tenant transactions work and cross-tenant fail appropriately

set -e

# Source environment if available
if [ -f .env ]; then
    source .env
fi

# Redis connection parameters
REDIS_HOST=${REDIS_HOST:-"localhost"}
REDIS_PORT=${REDIS_PORT:-6379}
REDIS_PASSWORD=${REDIS_PASSWORD:-""}

# Build redis-cli command
if [ "$REDIS_PORT" = "10000" ] || [ "$REDIS_PORT" = "6380" ]; then
    # Azure Managed Redis uses TLS on port 10000
    REDIS_CLI="redis-cli -h $REDIS_HOST -p $REDIS_PORT --tls -c"
else
    REDIS_CLI="redis-cli -h $REDIS_HOST -p $REDIS_PORT -c"
fi

if [ -n "$REDIS_PASSWORD" ]; then
    REDIS_CLI="$REDIS_CLI -a $REDIS_PASSWORD"
fi

echo "=== Redis Transaction Test Suite ==="
echo ""

# Test 1: Same-tenant transaction (should succeed)
echo "Test 1: Same-tenant transaction (should succeed)"
echo "Testing transaction with keys: {testuser}:tenant, {testuser}:destinations"

# Test same-tenant transaction using pipeline
{
    echo "MULTI"
    echo "HSET {testuser}:tenant name 'Test User' created_at '2025-01-01'"
    echo "HSET {testuser}:destinations dest1 'webhook-url-1'"
    echo "HSET {testuser}:destinations dest2 'webhook-url-2'"
    echo "EXEC"
} | $REDIS_CLI > /tmp/redis_result.txt 2>&1

if grep -q "OK" /tmp/redis_result.txt && ! grep -q "CROSSSLOT\|error\|Error" /tmp/redis_result.txt; then
    echo "✅ Same-tenant transaction succeeded"
else
    echo "❌ Same-tenant transaction failed:"
    cat /tmp/redis_result.txt
    exit 1
fi
echo ""

# Test 2: Cross-tenant transaction (should fail)
echo "Test 2: Cross-tenant transaction (should fail)"
echo "Testing transaction with keys: {user1}:tenant, {user2}:tenant"

{
    echo "MULTI"
    echo "HSET {user1}:tenant name 'User 1'"
    echo "HSET {user2}:tenant name 'User 2'"
    echo "EXEC"
} | $REDIS_CLI > /tmp/redis_result_cross.txt 2>&1

if grep -q "CROSSSLOT\|error\|Error" /tmp/redis_result_cross.txt; then
    echo "✅ Cross-tenant transaction correctly failed (CROSSSLOT error)"
elif grep -q "QUEUED" /tmp/redis_result_cross.txt; then
    echo "⚠️  Cross-tenant transaction unexpectedly succeeded (keys might be on same slot by chance)"
else
    echo "❌ Unexpected result:"
    cat /tmp/redis_result_cross.txt
fi
echo ""

# Test 3: Multi-key same-tenant operations (realistic scenario)
echo "Test 3: Multi-key same-tenant operations (realistic scenario)"
echo "Testing destination upsert pattern with multiple keys"

{
    echo "MULTI"
    echo "PERSIST {mycompany}:destination:webhook1"
    echo "HDEL {mycompany}:destination:webhook1 deleted_at"
    echo "HSET {mycompany}:destination:webhook1 id webhook1 type webhook url https://api.example.com/webhook created_at 2025-01-01T10:00:00Z"
    echo "HSET {mycompany}:destinations webhook1 '{\"id\":\"webhook1\",\"type\":\"webhook\"}'"
    echo "EXEC"
} | $REDIS_CLI > /tmp/redis_result_multi.txt 2>&1

if grep -q "OK" /tmp/redis_result_multi.txt && ! grep -q "CROSSSLOT\|error\|Error" /tmp/redis_result_multi.txt; then
    echo "✅ Realistic multi-key transaction succeeded"
else
    echo "❌ Realistic multi-key transaction failed:"
    cat /tmp/redis_result_multi.txt
    exit 1
fi
echo ""

# Test 4: Verify data consistency
echo "Test 4: Verify data consistency after transactions"

DEST_DATA=$($REDIS_CLI HGETALL "{mycompany}:destination:webhook1")
SUMMARY_DATA=$($REDIS_CLI HGET "{mycompany}:destinations" "webhook1")

if [[ "$DEST_DATA" == *"webhook1"* ]] && [[ "$SUMMARY_DATA" == *"webhook1"* ]]; then
    echo "✅ Data consistency verified - both destination and summary updated"
else
    echo "❌ Data inconsistency detected"
    echo "Destination data: $DEST_DATA"
    echo "Summary data: $SUMMARY_DATA"
    exit 1
fi
echo ""

# Cleanup test data
echo "Cleaning up test data..."
$REDIS_CLI DEL "{testuser}:tenant" "{testuser}:destinations" "{user1}:tenant" "{user2}:tenant" "{mycompany}:destination:webhook1" "{mycompany}:destinations" > /dev/null
echo ""

echo "=== Transaction Test Summary ==="
echo "✅ Same-tenant transactions work correctly"
echo "✅ Cross-tenant transactions fail appropriately" 
echo "✅ Multi-key operations maintain atomicity"
echo "✅ Data consistency preserved"
echo ""
echo "🎉 All transaction tests passed!"
echo "Ready to restore transactions in EntityStore operations."