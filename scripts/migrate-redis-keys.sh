#!/bin/bash

# Redis Key Migration Script
# Migrates from legacy key format (tenant:*) to hash-tagged format (tenant:{id}:*)
# This enables Redis cluster transactions while preserving existing data

set -e

# Check for help flag
if [ "$1" = "-help" ] || [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    cat << 'EOF'
Redis Key Migration Script
Migrates from legacy key format (tenant:*) to hash-tagged format (tenant:{id}:*)
This enables Redis cluster transactions while preserving existing data

USAGE:
    ./scripts/migrate-redis-keys.sh

DESCRIPTION:
    Scans for all legacy tenant keys in format "tenant:<id>" and migrates them
    to Redis cluster-compatible format "tenant:{<id>}" using hash tags.

    The migration is non-destructive - original keys are preserved and must be
    manually deleted after verifying the migration was successful.

ENVIRONMENT VARIABLES:
    REDIS_HOST          Redis server hostname (default: localhost)
    REDIS_PORT          Redis server port (default: 6379)
                        Auto-enables TLS if port is 10000 or 6380
    REDIS_PASSWORD      Redis password for authentication (default: none)

    Note: Script will source .env file if present in current directory

EXAMPLES:
    # Use default settings or .env file
    ./scripts/migrate-redis-keys.sh

    # Connect to specific Redis instance
    REDIS_HOST=redis.example.com REDIS_PORT=6379 ./scripts/migrate-redis-keys.sh

    # With authentication
    REDIS_PASSWORD=mypassword ./scripts/migrate-redis-keys.sh

    # For Azure Redis (TLS enabled)
    REDIS_HOST=myredis.redis.cache.windows.net REDIS_PORT=6380 REDIS_PASSWORD=key ./scripts/migrate-redis-keys.sh

WHAT IT MIGRATES:
    tenant:<id>                     → tenant:{<id>}
    tenant:<id>:destinations        → tenant:{<id>}:destinations
    tenant:<id>:destination:<dest>  → tenant:{<id>}:destination:<dest>

OUTPUT:
    - Shows count of tenants found
    - Progress for each tenant migration
    - Summary of migrated/skipped/failed counts
    - Next steps for cleanup after verification

EXIT CODES:
    0   Success - all migrations completed (or no migrations needed)
    1   Failure - one or more migrations failed

CLEANUP (after verification):
    redis-cli KEYS 'tenant:*' | xargs redis-cli DEL
EOF
    exit 0
fi

# Source environment if available
if [ -f .env ]; then
    source .env
fi

echo "=== Redis Key Format Migration ==="
echo "Migrating from legacy format to cluster-compatible hash-tagged format"
echo ""

# Redis connection parameters
REDIS_HOST=${REDIS_HOST:-"localhost"}
REDIS_PORT=${REDIS_PORT:-6379}
REDIS_PASSWORD=${REDIS_PASSWORD:-""}

# Build redis-cli command
if [ "$REDIS_PORT" = "10000" ] || [ "$REDIS_PORT" = "6380" ]; then
    REDIS_CLI="redis-cli -h $REDIS_HOST -p $REDIS_PORT --tls -c"
else
    REDIS_CLI="redis-cli -h $REDIS_HOST -p $REDIS_PORT -c"
fi

if [ -n "$REDIS_PASSWORD" ]; then
    REDIS_CLI="$REDIS_CLI -a $REDIS_PASSWORD"
fi

echo "🔍 Scanning for legacy tenant keys..."

# Find all legacy tenant keys
LEGACY_TENANTS=$($REDIS_CLI KEYS "tenant:*" | grep -E "^tenant:[^:]+$" | sed 's/tenant://' || true)

if [ -z "$LEGACY_TENANTS" ]; then
    echo "✅ No legacy tenant keys found. Migration not needed."
    exit 0
fi

echo "Found legacy tenants:"
for tenant in $LEGACY_TENANTS; do
    echo "  - $tenant"
done
echo ""

MIGRATED_COUNT=0
SKIPPED_COUNT=0
ERROR_COUNT=0

for TENANT_ID in $LEGACY_TENANTS; do
    echo "🔄 Migrating tenant: $TENANT_ID"
    
    # Check if already migrated
    NEW_EXISTS=$($REDIS_CLI EXISTS "tenant:{$TENANT_ID}")
    if [ "$NEW_EXISTS" = "1" ]; then
        echo "  ⏭️  Already migrated, skipping"
        SKIPPED_COUNT=$((SKIPPED_COUNT + 1))
        continue
    fi
    
    # Get legacy data
    LEGACY_TENANT_DATA=$($REDIS_CLI HGETALL "tenant:$TENANT_ID" 2>/dev/null || echo "")
    LEGACY_DEST_SUMMARY=$($REDIS_CLI HGETALL "tenant:$TENANT_ID:destinations" 2>/dev/null || echo "")
    LEGACY_DEST_IDS=$($REDIS_CLI HKEYS "tenant:$TENANT_ID:destinations" 2>/dev/null || echo "")
    
    if [ -z "$LEGACY_TENANT_DATA" ]; then
        echo "  ⚠️  No legacy tenant data found, skipping"
        SKIPPED_COUNT=$((SKIPPED_COUNT + 1))
        continue
    fi
    
    echo "  📊 Found tenant data and $(echo "$LEGACY_DEST_IDS" | wc -w) destinations"
    
    # Start migration transaction
    {
        echo "MULTI"
        
        # Migrate tenant data
        if [ -n "$LEGACY_TENANT_DATA" ]; then
            echo "HMSET tenant:{$TENANT_ID} $LEGACY_TENANT_DATA"
        fi

        # Migrate destination summary
        if [ -n "$LEGACY_DEST_SUMMARY" ]; then
            echo "HMSET tenant:{$TENANT_ID}:destinations $LEGACY_DEST_SUMMARY"
        fi
        
        # Migrate individual destinations
        for dest_id in $LEGACY_DEST_IDS; do
            if [ -n "$dest_id" ]; then
                DEST_DATA=$($REDIS_CLI HGETALL "tenant:$TENANT_ID:destination:$dest_id" 2>/dev/null || echo "")
                if [ -n "$DEST_DATA" ]; then
                    echo "HMSET tenant:{$TENANT_ID}:destination:$dest_id $DEST_DATA"
                fi
            fi
        done
        
        echo "EXEC"
    } | $REDIS_CLI > /tmp/migration_result.txt 2>&1
    
    # Check migration result
    if grep -q "OK" /tmp/migration_result.txt && ! grep -q "error\|Error\|ERR" /tmp/migration_result.txt; then
        echo "  ✅ Migration successful"
        MIGRATED_COUNT=$((MIGRATED_COUNT + 1))
        
        # Verify new data exists
        NEW_TENANT_EXISTS=$($REDIS_CLI EXISTS "tenant:{$TENANT_ID}")
        if [ "$NEW_TENANT_EXISTS" = "1" ]; then
            echo "  ✅ Verification passed"
        else
            echo "  ❌ Verification failed - new data not found"
            ERROR_COUNT=$((ERROR_COUNT + 1))
        fi
    else
        echo "  ❌ Migration failed:"
        cat /tmp/migration_result.txt
        ERROR_COUNT=$((ERROR_COUNT + 1))
    fi
    
    echo ""
done

echo "=== Migration Summary ==="
echo "✅ Successfully migrated: $MIGRATED_COUNT tenants"
echo "⏭️  Already migrated: $SKIPPED_COUNT tenants"
echo "❌ Failed migrations: $ERROR_COUNT tenants"
echo ""

if [ "$ERROR_COUNT" -gt 0 ]; then
    echo "⚠️  Some migrations failed. Please review errors above."
    exit 1
else
    echo "🎉 Migration completed successfully!"
    echo ""
    echo "📋 Next steps:"
    echo "1. Test the application with both legacy and new data"
    echo "2. Monitor for any issues"
    echo "3. After confirming stability, clean up legacy keys"
    echo ""
    echo "To clean up legacy keys (CAREFUL!):"
    echo "  redis-cli KEYS 'tenant:*' | xargs redis-cli DEL"
fi