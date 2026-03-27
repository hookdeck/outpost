# Issue #680: Scope Redis control plane keys by deployment ID

## Problem

Control plane keys (`outpostrc`, `.outpost:migration:lock`, `outpost:migration:*`) are not prefixed with `DEPLOYMENT_ID`, so multiple deployments sharing the same Dragonfly instance collide on these keys.

## Files

- `migrate.sh` — Production migration script for Dragonfly Cloud. Copies shared (unscoped) control plane keys to deployment-scoped versions. Dry run by default, `--apply` to execute, `--cleanup` to delete old shared keys after.
- `TEST_CASES.md` — Manual QA test cases for verifying the fix.

## Production Runbook (Dragonfly Cloud)

### Pre-flight

```bash
# Verify .envrc is loaded (direnv)
echo $OUTPOST_PROD_DRAGONFLY_HOST

# Verify connectivity
redis-cli -h "$OUTPOST_PROD_DRAGONFLY_HOST" -p "$OUTPOST_PROD_DRAGONFLY_PORT" \
  --user "$OUTPOST_PROD_DRAGONFLY_USER" --pass "$OUTPOST_PROD_DRAGONFLY_PASSWORD" \
  --tls --no-auth-warning PING
```

### Inspect current state

```bash
# See what shared keys exist
redis-cli ... KEYS "outpostrc"
redis-cli ... KEYS "outpost:migration:*"
redis-cli ... KEYS ".outpost:migration:*"
```

### Run migration

```bash
# 1. Dry run first
./migrate.sh

# 2. Review output, then apply
./migrate.sh --apply

# 3. Verify scoped keys exist for each deployment
redis-cli ... KEYS "tm_*:outpost:*"

# 4. Deploy new Outpost version to all Railway services

# 5. Verify deployments start cleanly (check Railway logs)

# 6. Clean up old shared keys
./migrate.sh --apply --cleanup
```

### Rollback

Before cleanup: new code reads scoped keys, old keys still exist. Rolling back Outpost version will use old unscoped keys untouched.

After cleanup: old keys are gone. Would need to redeploy old version and let it re-initialize.
