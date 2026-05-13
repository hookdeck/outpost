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
#   3. Optionally deletes the old shared keys (with --cleanup flag)
#
# All writes are batched into a single redis-cli --pipe call for performance.
# This avoids per-command TLS handshake overhead on remote Redis instances.
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

rcli_pipe() {
  local args=(-h "$REDIS_HOST" -p "$REDIS_PORT" --no-auth-warning --pipe)
  [[ -n "$REDIS_USER" ]] && args+=(--user "$REDIS_USER")
  [[ -n "$REDIS_PASS" ]] && args+=(--pass "$REDIS_PASS")
  [[ "$REDIS_TLS" == "1" ]] && args+=(--tls)
  redis-cli "${args[@]}"
}

# --- Helpers ---
log()  { echo "[INFO]  $*"; }
warn() { echo "[WARN]  $*" >&2; }

# --- Step 1: Discover deployment IDs ---
log "Discovering deployment IDs..."

DEPLOYMENT_IDS=()
cursor=0
while true; do
  result=$(rcli SCAN "$cursor" MATCH "*:tenant:*:tenant" COUNT 1000)
  cursor=$(echo "$result" | head -1)
  keys=$(echo "$result" | tail -n +2)

  for key in $keys; do
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

INSTALLATION_ID=$(rcli HGET outpostrc installation 2>/dev/null || echo "")
if [[ -n "$INSTALLATION_ID" ]]; then
  log "  outpostrc -> installation = $INSTALLATION_ID"
else
  log "  outpostrc -> (not found, each deployment will generate its own on startup)"
fi

# Collect migration keys and their hash data
declare -A MIGRATION_DATA
MIGRATION_KEYS=()
cursor=0
while true; do
  result=$(rcli SCAN "$cursor" MATCH "outpost:migration:*" COUNT 100)
  cursor=$(echo "$result" | head -1)
  keys=$(echo "$result" | tail -n +2)

  for key in $keys; do
    MIGRATION_KEYS+=("$key")
    MIGRATION_DATA["$key"]=$(rcli HGETALL "$key" 2>/dev/null | tr '\n' ' ')
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

LOCK_EXISTS=$(rcli EXISTS ".outpost:migration:lock" 2>/dev/null || echo "0")
if [[ "$LOCK_EXISTS" == "1" ]]; then
  warn "  .outpost:migration:lock exists! A migration may be running."
fi

# --- Step 3: Build pipeline commands ---
log ""
log "=== Building migration commands ==="

PIPE_FILE=$(mktemp)
CMD_COUNT=0

for DEPLOY_ID in "${DEPLOYMENT_IDS[@]}"; do
  # Installation ID (SET is idempotent)
  if [[ -n "$INSTALLATION_ID" ]]; then
    target_key="${DEPLOY_ID}:outpost:installation_id"
    echo "SET ${target_key} ${INSTALLATION_ID}" >> "$PIPE_FILE"
    ((++CMD_COUNT))
  fi

  # Migration status keys (HSET is idempotent)
  for old_key in "${MIGRATION_KEYS[@]}"; do
    new_key="${DEPLOY_ID}:${old_key}"
    hash_data="${MIGRATION_DATA[$old_key]}"
    if [[ -n "$hash_data" ]]; then
      echo "HSET ${new_key} ${hash_data}" >> "$PIPE_FILE"
      ((++CMD_COUNT))
    fi
  done
done

# Cleanup commands
if $CLEANUP; then
  if [[ -n "$INSTALLATION_ID" ]]; then
    echo "DEL outpostrc" >> "$PIPE_FILE"
    ((++CMD_COUNT))
  fi
  for key in "${MIGRATION_KEYS[@]}"; do
    echo "DEL ${key}" >> "$PIPE_FILE"
    ((++CMD_COUNT))
  done
  if [[ "$LOCK_EXISTS" == "1" ]]; then
    echo "DEL .outpost:migration:lock" >> "$PIPE_FILE"
    ((++CMD_COUNT))
  fi
fi

log "  ${CMD_COUNT} commands for ${#DEPLOYMENT_IDS[@]} deployments"

# --- Step 4: Execute or dry-run ---
if $DRY_RUN; then
  log ""
  log "=== Dry run — commands that would be sent ==="
  cat "$PIPE_FILE"
  log ""
  log "Dry run complete. Re-run with --apply to execute."
else
  log ""
  log "=== Executing pipeline ==="
  rcli_pipe < "$PIPE_FILE"
  log ""

  # Verify
  migrated=$(rcli --scan --pattern "*:outpost:installation_id" | wc -l | tr -d ' ')
  shared_remaining=$(rcli KEYS "outpostrc" 2>/dev/null | wc -l | tr -d ' ')
  log "Deployments with installation_id: ${migrated}"
  if $CLEANUP; then
    log "Shared keys remaining: ${shared_remaining} (should be 0)"
  fi
  log "Migration complete."
fi

rm -f "$PIPE_FILE"
