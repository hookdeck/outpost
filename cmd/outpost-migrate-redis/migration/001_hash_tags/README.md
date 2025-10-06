# Migration 001: Hash Tags

Migrates Redis keys from legacy format to hash-tagged format for Redis Cluster compatibility.

## Overview

Redis Cluster requires all keys in a transaction to be on the same hash slot. Without hash tags, keys like `tenant:123` and `tenant:123:destinations` could end up on different nodes, causing CROSSSLOT errors during transactions.

This migration adds hash tags `{...}` to ensure all keys for a tenant stay on the same Redis node, enabling atomic operations.

**Key format change:**
- `tenant:123` → `tenant:{123}:tenant`
- `tenant:123:destinations` → `tenant:{123}:destinations`
- `tenant:123:destination:abc` → `tenant:{123}:destination:abc`

## Migration Phases

### Plan
Scans for all legacy keys matching `tenant:*` pattern and counts:
- Number of unique tenants
- Number of destination summaries
- Total keys to migrate

### Apply
For each tenant found:
1. Copies tenant hash from `tenant:ID` to `tenant:{ID}:tenant`
2. Copies destination summary from `tenant:ID:destinations` to `tenant:{ID}:destinations`
3. Copies each destination from `tenant:ID:destination:X` to `tenant:{ID}:destination:X`
4. Uses transactions where possible for atomicity
5. Preserves original keys (no deletion during apply)

### Verify
Performs comprehensive spot checks on up to 20 random tenants:
- Confirms new tenant key exists (`tenant:{ID}:tenant`)
- Validates tenant data integrity by comparing field counts
- Checks destinations summary key (`tenant:{ID}:destinations`)
- Verifies each individual destination key (`tenant:{ID}:destination:X`)
- In verbose mode, performs deep field-by-field comparison
- Only counts as passed if ALL related keys are properly migrated

### Cleanup
After verification, removes all legacy keys:
- Deletes original `tenant:*` pattern keys
- Requires confirmation unless `-force` flag is used
- Processes deletions in batches of 100 keys

## Deployment Mode Compatibility

### When This Migration is NOT Needed

If you are using the `DEPLOYMENT_ID` configuration option (or `deployment_id` in YAML), **you can skip this migration entirely**. Deployments using deployment IDs already have keys in the correct format:

```
deployment:dp_001:tenant:{123}:tenant
deployment:dp_001:tenant:{123}:destinations
deployment:dp_001:tenant:{123}:destination:abc
```

These keys already include hash tags `{123}` and are Redis Cluster compatible.

### When This Migration IS Needed

This migration is only required for legacy deployments that:
1. Started before hash tag support was added
2. Are **NOT** using `DEPLOYMENT_ID` configuration
3. Have keys in the old format without curly braces:
   ```
   tenant:123
   tenant:123:destinations
   tenant:123:destination:abc
   ```

### Checking If You Need This Migration

Run the migration planner to check:
```bash
outpost-migrate-redis plan
```

If the output shows `0 tenants to migrate`, your deployment either:
- Already has hash tags (you're good!)
- Is using deployment IDs (you're good!)
- Has no data yet (you're good!)

## Notes

- Original keys are preserved during Apply phase for rollback safety
- Migration is idempotent - can be run multiple times safely
- Skips tenants that are already migrated
- Does not touch deployment-prefixed keys (`deployment:*`)