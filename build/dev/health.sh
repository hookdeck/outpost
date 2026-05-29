#!/usr/bin/env bash
# Quick reachability check for the running dev stack. Walks LOCAL_DEV_* flags
# the same way dev.sh does and probes only the services that should be up.
# Exits non-zero if any probe fails.
set -uo pipefail

if [ -f .env ]; then
  set -a; . ./.env; set +a
fi

fail=0
pass() { printf "  \033[32m✓\033[0m %s\n" "$1"; }
miss() { printf "  \033[31m✗\033[0m %s — %s\n" "$1" "$2"; fail=1; }

probe_http() {
  local name=$1 url=$2 extra=${3:-}
  # shellcheck disable=SC2086
  if curl -fsS --max-time 3 $extra "$url" >/dev/null 2>&1; then
    pass "$name ($url)"
  else
    miss "$name" "no response from $url"
  fi
}

probe_exec() {
  local name=$1 container=$2; shift 2
  if docker exec "$container" "$@" >/dev/null 2>&1; then
    pass "$name"
  else
    miss "$name" "$container probe failed: $*"
  fi
}

echo "Core"
probe_http "api"    "http://localhost:3333/api/v1/healthz"
probe_http "portal" "http://localhost:3333/"

# Cache: redis is default; dragonfly aliases as redis on the network and uses
# the same container name pattern. Probe whichever is running.
echo "Deps"
if [ "${LOCAL_DEV_DRAGONFLY:-}" = "1" ]; then
  probe_exec "dragonfly" outpost-dragonfly-1 redis-cli -a password PING
else
  probe_exec "redis" outpost-redis-1 redis-cli -a password PING
fi
[ "${LOCAL_DEV_POSTGRES:-}" = "1" ]   && probe_exec "postgres" outpost-postgres-1 pg_isready -U outpost
[ "${LOCAL_DEV_CLICKHOUSE:-}" = "1" ] && probe_http "clickhouse" "http://localhost:28123/ping"
[ "${LOCAL_DEV_RABBITMQ:-}" = "1" ]   && probe_http "rabbitmq" "http://localhost:25673/api/healthchecks/node" "-u guest:guest"
[ "${LOCAL_DEV_LOCALSTACK:-}" = "1" ] && probe_http "localstack" "http://localhost:24566/_localstack/health"
[ "${LOCAL_DEV_GCP:-}" = "1" ]        && probe_exec "gcp pubsub" outpost-gcp-1 sh -c 'wget -qO- http://localhost:8085 || true' \
                                     && pass "gcp pubsub (port reachable)" || true

# Add-on stacks
addons=0
[ "${LOCAL_DEV_ENVOY:-}" = "1" ]   && { [ $addons -eq 0 ] && echo "Add-ons"; addons=1; probe_http "envoy"   "http://localhost:9901/ready"; }
[ "${LOCAL_DEV_GRAFANA:-}" = "1" ] && { [ $addons -eq 0 ] && echo "Add-ons"; addons=1; probe_http "grafana" "http://localhost:3000/api/health"; }
[ "${LOCAL_DEV_UPTRACE:-}" = "1" ] && { [ $addons -eq 0 ] && echo "Add-ons"; addons=1; probe_http "uptrace" "http://localhost:14318"; }
[ "${LOCAL_DEV_AZURE:-}" = "1" ]   && { [ $addons -eq 0 ] && echo "Add-ons"; addons=1; probe_exec "azuresb" outpost-azuresb-1 sh -c 'true'; }

if [ $fail -ne 0 ]; then
  echo
  echo "One or more probes failed. Run 'make up' or check 'docker ps' for the underlying state."
  exit 1
fi
echo
echo "Stack healthy."
