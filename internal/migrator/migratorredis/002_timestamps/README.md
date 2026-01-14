# Migration 002: Timestamps

Converts timestamp fields from RFC3339 strings to Unix timestamps for timezone-agnostic sorting.

## Overview

Originally, Outpost stored `created_at` and `updated_at` as RFC3339 strings (e.g., `2024-01-15T10:30:00+07:00`). This had two issues:

1. **Should have used UTC** - Timestamps were stored with the server's local timezone offset, making them inconsistent across deployments in different regions
2. **Strings don't sort well** - RFC3339 strings with different timezone offsets don't sort correctly as strings, and aren't ideal for RediSearch numeric indexing

This migration converts timestamps to Unix format (seconds since epoch), which:
- Is timezone-agnostic (always represents the same instant)
- Sorts correctly as numeric values
- Works efficiently with RediSearch `NUMERIC SORTABLE` indexes

**Fields converted:**
| Model | Fields |
|-------|--------|
| Tenant | `created_at`, `updated_at` |
| Destination | `created_at`, `updated_at` |

**Note:** `disabled_at` is intentionally NOT migrated. It's not indexed by RediSearch (not needed for sorting), and lazy migration handles it safely through normal read/write operations.

## Migration Strategy

This migration uses a three-layer approach for safety and completeness:

```
┌─────────────────────────────────────────────────────────────────────────┐
│ Layer 1: AUTO-MIGRATION (at startup)                                    │
├─────────────────────────────────────────────────────────────────────────┤
│ - Runs automatically when Outpost starts                                │
│ - Uses SCAN (non-blocking) to iterate records                           │
│ - Converts timestamps on-the-fly                                        │
│ - Best-effort: records created during migration caught by lazy migration│
└─────────────────────────────────────────────────────────────────────────┘
                                    ↓
┌─────────────────────────────────────────────────────────────────────────┐
│ Layer 2: MANUAL MIGRATION (recommended for high-volume systems)         │
├─────────────────────────────────────────────────────────────────────────┤
│ outpost migrate apply 002_timestamps --rerun                            │
│                                                                         │
│ - Thorough scan of ALL records                                          │
│ - Catches records created/modified between auto-migration and startup   │
│ - --rerun flag allows re-running even if marked as "applied"            │
└─────────────────────────────────────────────────────────────────────────┘
                                    ↓
┌─────────────────────────────────────────────────────────────────────────┐
│ Layer 3: LAZY MIGRATION (ongoing)                                       │
├─────────────────────────────────────────────────────────────────────────┤
│ - parseTimestamp() reads both formats (numeric + RFC3339)               │
│ - On write, always stores as Unix timestamp                             │
│ - Gradually migrates remaining records through normal operations        │
└─────────────────────────────────────────────────────────────────────────┘
```

## Auto-Runnable

This migration runs automatically at startup (Layer 1). It's safe because:

- **In-place conversion** - Updates fields directly, no key renaming
- **Idempotent** - Skips records that already have numeric timestamps
- **Non-blocking** - Uses SCAN (cursor-based) instead of KEYS
- **Lazy migration fallback** - `parseTimestamp()` reads both formats, so any missed records still work and get converted on next write

## Re-running for High-Volume Systems

For systems with high write throughput, records may be created or modified between the auto-migration scan and when the application starts serving traffic. To ensure all records are migrated:

```bash
# Re-run to catch any records created during startup
outpost migrate apply 002_timestamps --rerun
```

**When to re-run:**

| Scenario | Why re-run helps |
|----------|------------------|
| After upgrading from an older version | Ensures all existing records are migrated immediately rather than waiting for lazy migration |
| Large dataset with active writes | Auto-migration scans sequentially; records created after the scan passes their key range won't be caught |
| After rolling deployment completes | During rollout, old pods may still write RFC3339 format until replaced |
| Verification shows pending records | `outpost migrate verify` reports records still needing migration |

**When you DON'T need to re-run:**
- Fresh deployments (no existing data)
- Low-traffic systems where lazy migration is acceptable
- If you're okay waiting for lazy migration to convert records on next write

Re-running is always safe due to idempotency - it simply skips already-converted records.

## Migration Phases

### Plan
Scans all tenant and destination records, identifies those with RFC3339 timestamps.

**Redis commands:**
```
SCAN 0 MATCH tenant:*:tenant COUNT 100           # Iterate tenant keys
SCAN 0 MATCH tenant:*:destination:* COUNT 100    # Iterate destination keys
HMGET <key> created_at updated_at                # Read timestamp fields (pipelined)
```

With deployment ID (e.g., `dp_001`):
```
SCAN 0 MATCH dp_001:tenant:*:tenant COUNT 100
SCAN 0 MATCH dp_001:tenant:*:destination:* COUNT 100
```

- Counts tenants needing migration
- Counts destinations needing migration
- Skips records already in numeric format

### Apply
Converts timestamps in batches of 100 using pipelining.

**Redis commands:**
```
HSET <key> created_at <unix_ts> updated_at <unix_ts>  # Update fields (pipelined)
```

- Updates `created_at` and `updated_at` fields in-place
- Converts RFC3339 string to Unix timestamp (int64)
- No keys are renamed or deleted

### Verify
Checks for any remaining RFC3339 timestamps.

**Redis commands:**
```
SCAN 0 MATCH tenant:*:tenant COUNT 100    # Same as Plan
HMGET <key> created_at updated_at         # Check if still RFC3339
```

- Reports count of records still needing migration
- Useful after auto-migration to check completeness

### Cleanup
No cleanup needed - timestamps are converted in-place.

## Checking Migration Status

```bash
# Show plan (counts records needing migration)
outpost migrate plan

# Verify after migration
outpost migrate verify
```

If verification shows pending records after auto-migration, run with `--rerun`:
```bash
outpost migrate apply 002_timestamps --rerun
```

## Notes

- Works with Redis, Redis Stack, Redis Cluster, and Dragonfly
- Compatible with deployment-prefixed keys (e.g., `dp_001:tenant:{123}:tenant`)
- Records created after migration automatically use Unix format (no migration needed)
