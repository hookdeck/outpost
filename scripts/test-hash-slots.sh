#!/bin/bash

# Hash Slot Validation Script for Redis Cluster
# Validates that tenant-related keys hash to the same slot

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
    REDIS_CLI="redis-cli -h $REDIS_HOST -p $REDIS_PORT --tls"
else
    REDIS_CLI="redis-cli -h $REDIS_HOST -p $REDIS_PORT"
fi

if [ -n "$REDIS_PASSWORD" ]; then
    REDIS_CLI="$REDIS_CLI -a $REDIS_PASSWORD"
fi

echo "=== Redis Cluster Hash Slot Validation ==="
echo "Testing tenant key patterns for hash slot distribution"
echo ""

# Test multiple tenant IDs to verify consistent behavior
TEST_TENANTS=("test123" "user456" "tenant789" "abc-def-123")

for TENANT_ID in "${TEST_TENANTS[@]}"; do
    echo "Testing tenant: $TENANT_ID"
    
    # Get hash slots for all tenant-related keys (using new hash tag format)
    TENANT_SLOT=$($REDIS_CLI CLUSTER KEYSLOT "{$TENANT_ID}:tenant")
    DEST_SUMMARY_SLOT=$($REDIS_CLI CLUSTER KEYSLOT "{$TENANT_ID}:destinations")
    DEST1_SLOT=$($REDIS_CLI CLUSTER KEYSLOT "{$TENANT_ID}:destination:dest1")
    DEST2_SLOT=$($REDIS_CLI CLUSTER KEYSLOT "{$TENANT_ID}:destination:dest2")
    DEST3_SLOT=$($REDIS_CLI CLUSTER KEYSLOT "{$TENANT_ID}:destination:webhook-endpoint")
    
    echo "  {$TENANT_ID}:tenant                         -> slot $TENANT_SLOT"
    echo "  {$TENANT_ID}:destinations                   -> slot $DEST_SUMMARY_SLOT"
    echo "  {$TENANT_ID}:destination:dest1              -> slot $DEST1_SLOT"
    echo "  {$TENANT_ID}:destination:dest2              -> slot $DEST2_SLOT"
    echo "  {$TENANT_ID}:destination:webhook-endpoint   -> slot $DEST3_SLOT"
    
    # Verify all slots are the same
    if [ "$TENANT_SLOT" = "$DEST_SUMMARY_SLOT" ] && \
       [ "$TENANT_SLOT" = "$DEST1_SLOT" ] && \
       [ "$TENANT_SLOT" = "$DEST2_SLOT" ] && \
       [ "$TENANT_SLOT" = "$DEST3_SLOT" ]; then
        echo "  ✅ All keys hash to same slot: $TENANT_SLOT"
    else
        echo "  ❌ Keys hash to different slots - transactions will fail!"
        exit 1
    fi
    echo ""
done

echo "=== Cross-Tenant Slot Validation ==="
echo "Verifying different tenants use different slots (expected behavior)"
echo ""

# Test that different tenants get different slots
TENANT1_SLOT=$($REDIS_CLI CLUSTER KEYSLOT "{user1}:tenant")
TENANT2_SLOT=$($REDIS_CLI CLUSTER KEYSLOT "{user2}:tenant")
TENANT3_SLOT=$($REDIS_CLI CLUSTER KEYSLOT "{user3}:tenant")

echo "{user1}:tenant -> slot $TENANT1_SLOT"
echo "{user2}:tenant -> slot $TENANT2_SLOT" 
echo "{user3}:tenant -> slot $TENANT3_SLOT"

if [ "$TENANT1_SLOT" != "$TENANT2_SLOT" ] || [ "$TENANT1_SLOT" != "$TENANT3_SLOT" ]; then
    echo "✅ Different tenants use different slots (good for distribution)"
else
    echo "⚠️  All tenants hash to same slot (unusual but not wrong)"
fi

echo ""
echo "=== Cluster Info ==="
$REDIS_CLI CLUSTER INFO | grep -E "(cluster_state|cluster_slots_assigned)"
echo ""

echo "=== Summary ==="
echo "✅ Hash slot validation completed successfully"
echo "✅ Same-tenant keys will support transactions"
echo "✅ Cross-tenant operations will correctly fail in transactions"