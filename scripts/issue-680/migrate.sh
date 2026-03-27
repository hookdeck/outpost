#!/usr/bin/env bash
#
# Migration script for issue #680: Scope Redis control plane keys by deployment ID.
#
# This script copies the shared (unscoped) control plane keys to deployment-scoped
# versions for each deployment that exists in Redis.
#
# What it does:
#   1. Discovers all deployment IDs by scanning for {id}:tenant:* key patterns
#   2. For each deployment ID:
#      a. Copies outpostrc hash → {id}:outpost:installation_id (string key)
#      b. Copies outpost:migration:{name} hashes → {id}:outpost:migration:{name}
#      c. Copies outpost:migration:{name}:run:* hashes → {id}:outpost:migration:{name}:run:*
#   3. Optionally deletes the old shared keys (with --cleanup flag)
#
# Usage:
#   # Dry run (default) - shows what would be done
#   ./migrate.sh
#
#   # Actually run the migration
#   ./migrate.sh --apply
#
#   # Apply + clean up old shared keys after
#   ./migrate.sh --apply --cleanup
#
# Connection env vars:
#   REDIS_HOST (required)
#   REDIS_PORT (default: 6379)
#   REDIS_USER (optional)
#   REDIS_PASSWORD (optional)
#   REDIS_TLS (set to "1" to enable TLS)
#

set -euo pipefail

# --- Config ---
DRY_RUN=true
CLEANUP=false

for arg in "$@"; do
  case "$arg" in
    --apply)  DRY_RUN=false ;;
    --cleanup) CLEANUP=true ;;
    --help|-h)
      echo "Usage: $0 [--apply] [--cleanup]"
      echo "  --apply    Actually run the migration (default: dry run)"
      echo "  --cleanup  Delete old shared keys after migration"
      exit 0
      ;;
  esac
done

# --- Connection ---
REDIS_HOST="${REDIS_HOST:?Set REDIS_HOST}"
REDIS_PORT="${REDIS_PORT:-6379}"
REDIS_USER="${REDIS_USER:-}"
REDIS_PASS="${REDIS_PASSWORD:-}"
REDIS_TLS="${REDIS_TLS:-}"

rcli() {
  local args=(-h "$REDIS_HOST" -p "$REDIS_PORT" --no-auth-warning)
  [[ -n "$REDIS_USER" ]] && args+=(--user "$REDIS_USER")
  [[ -n "$REDIS_PASS" ]] && args+=(--pass "$REDIS_PASS")
  [[ "$REDIS_TLS" == "1" ]] && args+=(--tls)
  redis-cli "${args[@]}" "$@"
}

# --- Helpers ---
log()  { echo "[INFO]  $*"; }
warn() { echo "[WARN]  $*" >&2; }
dry()  { if $DRY_RUN; then echo "[DRY]   $*"; else echo "[EXEC]  $*"; fi; }

# --- Step 1: Discover deployment IDs ---
log "Discovering deployment IDs..."

# Scan for keys matching *:tenant:* and extract unique prefixes
DEPLOYMENT_IDS=()
cursor=0
while true; do
  result=$(rcli SCAN "$cursor" MATCH "*:tenant:*:tenant" COUNT 1000)
  cursor=$(echo "$result" | head -1)
  keys=$(echo "$result" | tail -n +2)

  for key in $keys; do
    # Extract deployment ID (everything before first ":tenant:")
    id="${key%%:tenant:*}"
    if [[ -n "$id" && "$id" != "$key" ]]; then
      DEPLOYMENT_IDS+=("$id")
    fi
  done

  if [[ "$cursor" == "0" ]]; then
    break
  fi
done

# Deduplicate
DEPLOYMENT_IDS=($(printf '%s\n' "${DEPLOYMENT_IDS[@]}" | sort -u))

if [[ ${#DEPLOYMENT_IDS[@]} -eq 0 ]]; then
  warn "No deployment IDs found. Nothing to migrate."
  exit 0
fi

log "Found ${#DEPLOYMENT_IDS[@]} deployment(s): ${DEPLOYMENT_IDS[*]}"

# --- Step 2: Check current state of shared keys ---
log ""
log "Checking shared control plane keys..."

# Check outpostrc
INSTALLATION_ID=$(rcli HGET outpostrc installation 2>/dev/null || echo "")
if [[ -n "$INSTALLATION_ID" ]]; then
  log "  outpostrc -> installation = $INSTALLATION_ID"
else
  log "  outpostrc -> (not found, each deployment will generate its own on startup)"
fi

# Find all outpost:migration:* keys (excluding run history)
MIGRATION_KEYS=()
cursor=0
while true; do
  result=$(rcli SCAN "$cursor" MATCH "outpost:migration:*" COUNT 100)
  cursor=$(echo "$result" | head -1)
  keys=$(echo "$result" | tail -n +2)

  for key in $keys; do
    MIGRATION_KEYS+=("$key")
  done

  if [[ "$cursor" == "0" ]]; then
    break
  fi
done

log "  Found ${#MIGRATION_KEYS[@]} migration key(s)"
for key in "${MIGRATION_KEYS[@]}"; do
  status=$(rcli HGET "$key" status 2>/dev/null || echo "(no status)")
  log "    $key -> $status"
done

# Check lock key
LOCK_EXISTS=$(rcli EXISTS ".outpost:migration:lock" 2>/dev/null || echo "0")
if [[ "$LOCK_EXISTS" == "1" ]]; then
  warn "  .outpost:migration:lock exists! A migration may be running."
fi

# --- Step 3: Migrate for each deployment ---
log ""
log "=== Starting migration ==="

for DEPLOY_ID in "${DEPLOYMENT_IDS[@]}"; do
  log ""
  log "--- Deployment: $DEPLOY_ID ---"

  # 3a. Installation ID
  if [[ -n "$INSTALLATION_ID" ]]; then
    target_key="${DEPLOY_ID}:outpost:installation_id"
    existing=$(rcli GET "$target_key" 2>/dev/null || echo "")
    if [[ -n "$existing" ]]; then
      log "  $target_key already exists ($existing), skipping"
    else
      dry "  SET $target_key $INSTALLATION_ID"
      if ! $DRY_RUN; then
        rcli SET "$target_key" "$INSTALLATION_ID" > /dev/null
      fi
    fi
  fi

  # 3b. Migration status keys
  for old_key in "${MIGRATION_KEYS[@]}"; do
    # Derive the new key: outpost:migration:X -> {id}:outpost:migration:X
    new_key="${DEPLOY_ID}:${old_key}"

    existing=$(rcli EXISTS "$new_key" 2>/dev/null || echo "0")
    if [[ "$existing" == "1" ]]; then
      log "  $new_key already exists, skipping"
      continue
    fi

    # Copy the hash field by field
    dry "  COPY $old_key -> $new_key"
    if ! $DRY_RUN; then
      # Read all field-value pairs and write them to the new key
      hset_args=()
      while IFS= read -r field && IFS= read -r value; do
        hset_args+=("$field" "$value")
      done < <(rcli HGETALL "$old_key" 2>/dev/null)

      if [[ ${#hset_args[@]} -gt 0 ]]; then
        rcli HSET "$new_key" "${hset_args[@]}" > /dev/null
      else
        warn "  $old_key has no fields, skipping"
      fi

      # Verify the copy
      if [[ $(rcli EXISTS "$new_key" 2>/dev/null) != "1" ]]; then
        warn "  FAILED to copy $old_key -> $new_key"
      fi
    fi
  done
done

# --- Step 4: Cleanup (optional) ---
if $CLEANUP && ! $DRY_RUN; then
  log ""
  log "=== Cleaning up old shared keys ==="

  if [[ -n "$INSTALLATION_ID" ]]; then
    log "  DEL outpostrc"
    rcli DEL outpostrc > /dev/null
  fi

  for key in "${MIGRATION_KEYS[@]}"; do
    log "  DEL $key"
    rcli DEL "$key" > /dev/null
  done

  if [[ "$LOCK_EXISTS" == "1" ]]; then
    log "  DEL .outpost:migration:lock"
    rcli DEL ".outpost:migration:lock" > /dev/null
  fi
elif $CLEANUP && $DRY_RUN; then
  log ""
  log "=== Cleanup (dry run) ==="
  log "  Would delete: outpostrc, .outpost:migration:lock, and ${#MIGRATION_KEYS[@]} migration key(s)"
fi

log ""
if $DRY_RUN; then
  log "Dry run complete. Re-run with --apply to execute."
else
  log "Migration complete."
fi
