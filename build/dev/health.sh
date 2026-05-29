#!/usr/bin/env bash
# Reachability check for the running dev stack. Walks LOCAL_DEV_* flags
# the same way dev.sh does and probes only the services that should be up.
# Exits non-zero if any probe fails.
#
# WAIT=N polls for up to N seconds before declaring failure (useful right
# after `make up` while containers are still warming).
set -uo pipefail

if [ -f .env ]; then
  set -a; . ./.env; set +a
fi

WAIT="${WAIT:-0}"

probe_http() {
  local name=$1 url=$2 extra=${3:-}
  # shellcheck disable=SC2086
  if curl -fsS --max-time 3 $extra "$url" >/dev/null 2>&1; then
    printf "  \033[32m✓\033[0m %s (%s)\n" "$name" "$url"
  else
    printf "  \033[31m✗\033[0m %s — no response from %s\n" "$name" "$url"
    return 1
  fi
}

probe_exec() {
  local name=$1 container=$2; shift 2
  if docker exec "$container" "$@" >/dev/null 2>&1; then
    printf "  \033[32m✓\033[0m %s\n" "$name"
  else
    printf "  \033[31m✗\033[0m %s — %s probe failed: %s\n" "$name" "$container" "$*"
    return 1
  fi
}

run_probes() {
  local fail=0

  echo "Core"
  probe_http "api"    "http://localhost:3333/api/v1/healthz" || fail=1
  probe_http "portal" "http://localhost:3333/" || fail=1

  echo "Deps"
  if [ "${LOCAL_DEV_DRAGONFLY:-}" = "1" ]; then
    probe_exec "dragonfly" outpost-dragonfly-1 redis-cli -a password PING || fail=1
  else
    probe_exec "redis" outpost-redis-1 redis-cli -a password PING || fail=1
  fi
  [ "${LOCAL_DEV_POSTGRES:-}" = "1" ]   && { probe_exec "postgres" outpost-postgres-1 pg_isready -U outpost || fail=1; }
  [ "${LOCAL_DEV_CLICKHOUSE:-}" = "1" ] && { probe_http "clickhouse" "http://localhost:28123/ping" || fail=1; }
  [ "${LOCAL_DEV_RABBITMQ:-}" = "1" ]   && { probe_http "rabbitmq" "http://localhost:25673/api/healthchecks/node" "-u guest:guest" || fail=1; }
  [ "${LOCAL_DEV_LOCALSTACK:-}" = "1" ] && { probe_http "localstack" "http://localhost:24566/_localstack/health" || fail=1; }
  [ "${LOCAL_DEV_GCP:-}" = "1" ]        && { probe_exec "gcp pubsub" outpost-gcp-1 sh -c 'curl -sS http://localhost:8085 >/dev/null' || fail=1; }

  local addons=0
  show_addons() { [ $addons -eq 0 ] && echo "Add-ons"; addons=1; }
  [ "${LOCAL_DEV_ENVOY:-}" = "1" ]   && { show_addons; probe_http "envoy"   "http://localhost:9901/ready" || fail=1; }
  [ "${LOCAL_DEV_GRAFANA:-}" = "1" ] && { show_addons; probe_http "grafana" "http://localhost:3000/api/health" || fail=1; }
  [ "${LOCAL_DEV_UPTRACE:-}" = "1" ] && { show_addons; probe_http "uptrace" "http://localhost:14318" || fail=1; }
  [ "${LOCAL_DEV_AZURE:-}" = "1" ]   && { show_addons; probe_exec "azuresb" outpost-azuresb-1 sh -c 'true' || fail=1; }

  return $fail
}

deadline=$(( $(date +%s) + WAIT ))
attempt=0
while :; do
  attempt=$(( attempt + 1 ))
  output=$(run_probes)
  status=$?
  if [ $status -eq 0 ] || [ "$(date +%s)" -ge "$deadline" ]; then
    break
  fi
  sleep 1
done

echo "$output"
if [ $status -ne 0 ]; then
  echo
  if [ "$WAIT" -gt 0 ]; then
    echo "One or more probes failed after ${WAIT}s of polling. Check 'docker ps' for the underlying state."
  else
    echo "One or more probes failed. Try 'make health WAIT=10' if the stack just came up, or check 'docker ps'."
  fi
  exit 1
fi
echo
if [ $attempt -gt 1 ]; then
  echo "Stack healthy (after ${attempt} attempts)."
else
  echo "Stack healthy."
fi
