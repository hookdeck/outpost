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
URL="${OUTPOST_URL:-http://localhost:3333/api/v1}"
KEY="${OUTPOST_API_KEY:-apikey}"

probe_http() {
  local name=$1 url=$2 extra=${3:-}
  # shellcheck disable=SC2086
  if curl -fsS --max-time 3 $extra "$url" >/dev/null 2>&1; then
    printf "\033[32m✓\033[0m %s (%s)\n" "$name" "$url"
  else
    printf "\033[31m✗\033[0m %s — no response from %s\n" "$name" "$url"
    return 1
  fi
}

probe_exec() {
  local name=$1 container=$2; shift 2
  if docker exec "$container" "$@" >/dev/null 2>&1; then
    printf "\033[32m✓\033[0m %s\n" "$name"
  else
    printf "\033[31m✗\033[0m %s — %s probe failed: %s\n" "$name" "$container" "$*"
    return 1
  fi
}

run_probes() {
  local fail=0

  # api healthz transitively verifies that deps are reachable — api would
  # not have started otherwise (depends_on with service_healthy).
  probe_http "api" "$URL/healthz" || fail=1

  [ "${LOCAL_DEV_ENVOY:-}" = "1" ]   && { probe_http "envoy"   "http://localhost:9901/ready" || fail=1; }
  [ "${LOCAL_DEV_GRAFANA:-}" = "1" ] && { probe_http "grafana" "http://localhost:3000/api/health" || fail=1; }
  [ "${LOCAL_DEV_UPTRACE:-}" = "1" ] && { probe_http "uptrace" "http://localhost:14318" || fail=1; }
  [ "${LOCAL_DEV_AZURE:-}" = "1" ]   && { probe_exec "azuresb" outpost-azuresb-1 sh -c 'true' || fail=1; }

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
  if [ "$WAIT" -gt 0 ]; then
    echo "Not healthy after ${WAIT}s." >&2
  else
    echo "Not healthy. Try 'make health WAIT=10' if the stack just came up." >&2
  fi
  exit 1
fi
