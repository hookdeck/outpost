#!/usr/bin/env bash
# Drives `make up` / `make down`. Reads LOCAL_DEV_* from .env and assembles
# the docker compose invocation: list of -f files + COMPOSE_PROFILES.
#
# Usage: dev.sh up|down [extra docker compose args...]
set -euo pipefail

cmd="${1:-}"
shift || true

if [ -z "$cmd" ]; then
  echo "usage: dev.sh up|down [extra args]" >&2
  exit 2
fi

# Source .env so LOCAL_DEV_* vars are visible. Missing .env is fine — nothing
# add-on is enabled by default.
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

files=(
  -f build/dev/compose.yml
  -f build/dev/deps/compose.yml
  -f build/dev/deps/compose-gui.yml
)
profiles=()

# Cache: redis is default; dragonfly opts in and aliases as `redis` on the network.
if [ "${LOCAL_DEV_DRAGONFLY:-}" = "1" ]; then
  profiles+=(dragonfly)
else
  profiles+=(redis)
fi

# Log stores
[ "${LOCAL_DEV_POSTGRES:-}" = "1" ] && profiles+=(postgres)
[ "${LOCAL_DEV_CLICKHOUSE:-}" = "1" ] && profiles+=(clickhouse)

# Message queues
[ "${LOCAL_DEV_RABBITMQ:-}" = "1" ] && profiles+=(rabbitmq)
[ "${LOCAL_DEV_LOCALSTACK:-}" = "1" ] && profiles+=(localstack)
[ "${LOCAL_DEV_GCP:-}" = "1" ] && profiles+=(gcp)

# GUI tools (any non-empty value enables; value doubles as port via ${VAR:-default})
[ -n "${LOCAL_DEV_REDIS_COMMANDER:-}" ] && profiles+=(redis-commander)
[ -n "${LOCAL_DEV_PGADMIN:-}" ] && profiles+=(pgadmin)
[ -n "${LOCAL_DEV_TABIX:-}" ] && profiles+=(tabix)

# Add-on stacks: each is a separate compose file merged into the `outpost`
# project only when its flag is on. File-level inclusion (rather than service
# profiles) avoids cross-file service-name collisions, e.g. otel-collector
# defined by both grafana and uptrace.
[ "${LOCAL_DEV_ENVOY:-}" = "1" ] && files+=(-f build/dev/envoy/compose.yml)
[ "${LOCAL_DEV_GRAFANA:-}" = "1" ] && files+=(-f build/dev/grafana/compose.yml)
[ "${LOCAL_DEV_UPTRACE:-}" = "1" ] && files+=(-f build/dev/uptrace/compose.yml)
[ "${LOCAL_DEV_AZURE:-}" = "1" ] && files+=(-f build/dev/azure/compose.yml)

# grafana and uptrace both define otel-collector and overlap on OTLP ports.
# Enabling both at once is almost always a misconfiguration.
if [ "${LOCAL_DEV_GRAFANA:-}" = "1" ] && [ "${LOCAL_DEV_UPTRACE:-}" = "1" ]; then
  echo "error: LOCAL_DEV_GRAFANA and LOCAL_DEV_UPTRACE cannot both be enabled (they conflict on otel-collector + ports)" >&2
  exit 1
fi

COMPOSE_PROFILES="$(IFS=,; echo "${profiles[*]}")"
export COMPOSE_PROFILES

case "$cmd" in
  up)
    exec docker compose --env-file .env "${files[@]}" up -d "$@"
    ;;
  down)
    # --remove-orphans cleans up containers from add-on files that may have
    # been included previously but aren't in the current invocation.
    exec docker compose --env-file .env "${files[@]}" down --remove-orphans "$@"
    ;;
  *)
    echo "usage: dev.sh up|down [extra args]" >&2
    exit 2
    ;;
esac
