# Redis Key Format Migration

## Overview

This document outlines the migration strategy from legacy Redis key formats to cluster-compatible hash-tagged formats, enabling Redis cluster transactions while preserving existing data.

## The Problem

**Legacy Key Format:**
```
tenant:123                    → Different hash slots
tenant:123:destinations       → Cannot use transactions
tenant:123:destination:abc    → CROSSSLOT errors
```

**New Hash-Tagged Format:**
```
{123}:tenant                  → Same hash slot
{123}:destinations            → Enables transactions  
{123}:destination:abc         → Atomic operations
```

## Migration Strategy

### 1. Prerequisites

#### Required Configuration

The migration script requires Redis connection configuration via environment variables in `.env` file:

```bash
# Redis Connection Settings
REDIS_HOST="your-redis-host.example.com"
REDIS_PORT=10000                    # 10000 for Azure Managed Redis, 6379 for standard Redis
REDIS_PASSWORD="your-redis-password"
REDIS_DATABASE=0
REDIS_TLS_ENABLED=true             # true for Azure Managed Redis
REDIS_CLUSTER_ENABLED=true         # true for cluster mode
```

#### Azure Managed Redis Example:
```bash
REDIS_HOST="myapp-redis.westeurope.redisenterprise.cache.azure.net"
REDIS_PORT=10000
REDIS_PASSWORD="abc123xyz789=="
REDIS_DATABASE=0
REDIS_TLS_ENABLED=true
REDIS_CLUSTER_ENABLED=true
```

#### Standard Redis Example:
```bash
REDIS_HOST="localhost"
REDIS_PORT=6379
REDIS_PASSWORD=""
REDIS_DATABASE=0
REDIS_TLS_ENABLED=false
REDIS_CLUSTER_ENABLED=false
```

#### Required Tools
- **redis-cli**: Must be installed and accessible in PATH
- **Network access**: Connection to Redis instance on specified port
- **Authentication**: Valid credentials for Redis instance

#### Configuration Validation

Before running the migration script, test your Redis connection:

```bash
# Test basic connectivity
redis-cli -h $REDIS_HOST -p $REDIS_PORT --tls -a $REDIS_PASSWORD ping

# Test cluster mode (if enabled)
redis-cli -h $REDIS_HOST -p $REDIS_PORT --tls -a $REDIS_PASSWORD -c CLUSTER INFO

# Check for existing tenant data
redis-cli -h $REDIS_HOST -p $REDIS_PORT --tls -a $REDIS_PASSWORD KEYS "tenant:*"
```

Expected responses:
- `PONG` for ping test
- `cluster_state:ok` for cluster info
- List of tenant keys for data check

### 2. Pre-Migration Requirement

**IMPORTANT**: The migration script `scripts/migrate-redis-keys.sh` **MUST** be run before deploying the new application code.

The new code only supports the hash-tagged format and will not read legacy keys.

### 2. Migration Process

#### Step 1: Run Migration Script
```bash
./scripts/migrate-redis-keys.sh
```

This script:
- Scans for legacy tenant keys (`tenant:*`)
- Migrates each tenant's data to hash-tagged format (`{tenant}:*`)
- Uses transactions where possible for atomicity
- Provides detailed progress reporting
- Verifies migration success

#### Step 2: Deploy Updated Code
Deploy the new version that uses hash-tagged keys exclusively.

#### Step 3: Cleanup (Optional)
After confirming the application works correctly, remove legacy keys:
```bash
redis-cli KEYS 'tenant:*' | xargs redis-cli DEL
```

## Code Changes

### Key Generation Functions

**Before:**
```go
func redisTenantID(tenantID string) string {
    return fmt.Sprintf("tenant:%s", tenantID)
}
```

**After:**
```go
func redisTenantID(tenantID string) string {
    return fmt.Sprintf("{%s}:tenant", tenantID)
}
```

### Simplified Architecture

The application code now:
- **Only reads/writes** hash-tagged format
- **No backward compatibility layer** (cleaner code)
- **Assumes migration completed** before deployment

## Benefits

### Transaction Support Restored
- **UpsertDestination**: Atomic destination + summary updates
- **DeleteDestination**: Atomic deletion with summary cleanup  
- **DeleteTenant**: Atomic multi-destination deletion
- **Data Consistency**: No partial failures

### Cluster Compatibility
- Works with Azure Managed Redis cluster mode
- No CROSSSLOT or MOVED errors
- Proper hash slot distribution

### Simplified Codebase
- No dual-format complexity
- Clean, focused implementation
- Easier to maintain and debug

## Deployment Requirements

### Critical Order
1. **First**: Run migration script
2. **Second**: Deploy new application code
3. **Third**: Cleanup legacy keys (optional)

### Validation Steps

1. **Pre-Migration**: Verify legacy data exists
   ```bash
   redis-cli KEYS 'tenant:*'
   ```

2. **Migration**: Run migration script
   ```bash
   ./scripts/migrate-redis-keys.sh
   ```

3. **Post-Migration**: Verify new format data
   ```bash
   redis-cli KEYS '{*}:*'
   ```

4. **Application Testing**:
   ```bash
   ./examples/azure/diagnostics.sh
   ```

## Rollback Plan

If issues arise after deployment:

1. **Immediate**: Rollback to previous application version
2. **Data**: Legacy keys remain intact for safety
3. **Re-migration**: Can re-run migration script if needed

## Production Deployment Checklist

- [ ] **SETUP**: Configure Redis connection in `.env` file
- [ ] **VALIDATE**: Test Redis connectivity with `redis-cli ping`
- [ ] **VERIFY**: Check existing data with `redis-cli KEYS 'tenant:*'`
- [ ] **CRITICAL**: Run migration script BEFORE deployment
- [ ] Verify migration completed successfully
- [ ] Deploy new application code
- [ ] Test all critical operations
- [ ] Monitor application logs for Redis-related errors
- [ ] Verify transaction functionality works
- [ ] Monitor for 24-48 hours
- [ ] Clean up legacy keys (optional)

## Migration Script Safety Features

- **Idempotent**: Can be run multiple times safely
- **Verification**: Confirms new data exists after migration
- **Atomic**: Uses Redis transactions where possible
- **Progress Reporting**: Shows detailed migration status
- **Error Handling**: Stops on failures with clear error messages

## Emergency Recovery

If migration fails or data is corrupted:

1. **Legacy keys preserved**: Original data remains intact
2. **Re-run migration**: Script can be executed again
3. **Application rollback**: Previous version can still read legacy keys

The migration approach prioritizes **data safety** and **operational simplicity** over backward compatibility complexity.