#!/usr/bin/env bash
# Drives `make up` / `make down`. Reads LOCAL_DEV_* from .env and assembles
# the docker compose invocation: list of -f files + COMPOSE_PROFILES.
#
# Usage: dev.sh up|down|run [extra docker compose args...]
set -euo pipefail

cmd="${1:-}"
shift || true

if [ -z "$cmd" ]; then
  echo "usage: dev.sh up|down|run [extra args]" >&2
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

# Message queues. Starting a queue doesn't select it: Outpost uses the one
# configured under `mqs:` in .outpost.yaml (or the matching env vars).
[ "${LOCAL_DEV_NATS:-}" = "1" ] && profiles+=(nats)
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
[ "${LOCAL_DEV_ENVOY_CHAIN:-}" = "1" ] && files+=(-f build/dev/envoy-chain/compose.yml)
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

# check_queue_flags fails fast when the Outpost config points at a queue whose
# service isn't enabled: the api would otherwise wait ~2 minutes for it and
# fail with a bare i/o timeout. Checks the YAML file named by CONFIG and .env.
check_queue_flags() {
  local sources=(.env)
  [ -n "${CONFIG:-}" ] && [ -f "${CONFIG}" ] && sources+=("${CONFIG}")
  uses() { grep -hEv '^[[:space:]]*#' "${sources[@]}" 2>/dev/null | grep -qE "$1"; }
  local fail=0
  if uses 'amqp://[^"[:space:]]*@rabbitmq:' && [ "${LOCAL_DEV_RABBITMQ:-}" != "1" ]; then
    echo "error: your Outpost config uses RabbitMQ (rabbitmq:5672) but LOCAL_DEV_RABBITMQ=1 isn't set in .env." >&2
    fail=1
  fi
  if uses 'nats://nats:' && [ "${LOCAL_DEV_NATS:-}" != "1" ]; then
    echo "error: your Outpost config uses NATS (nats:4222) but LOCAL_DEV_NATS=1 isn't set in .env." >&2
    fail=1
  fi
  if [ "$fail" = "1" ]; then
    echo "Enable the queue in .env, or point mqs: in ${CONFIG:-.outpost.yaml} at an enabled one." >&2
    echo "Upgrading from a RabbitMQ setup? See \"Choosing the internal message queue\" in contributing/getting-started.md." >&2
    exit 1
  fi
}

dc() { docker compose --env-file .env "${files[@]}" "$@"; }

case "$cmd" in
  up)
    check_queue_flags
    # Shared with the destination stack (make up/dest); see compose.yml.
    docker network create outpost-dest >/dev/null 2>&1 || true
    # Services whose flag was turned off: compose leaves containers of
    # inactive profiles running, so remove them to match .env.
    inactive=$(comm -13 <(dc config --services | sort) <(COMPOSE_PROFILES="*" dc config --services | sort))
    if [ -n "$inactive" ]; then
      # shellcheck disable=SC2086
      COMPOSE_PROFILES="*" dc rm --stop --force $inactive >/dev/null 2>&1 || true
    fi
    exec docker compose --env-file .env "${files[@]}" up -d --remove-orphans "$@"
    ;;
  down)
    # All profiles, so services whose flag was turned off since `make up` go
    # too; --remove-orphans cleans up containers from add-on files that may
    # have been included previously but aren't in the current invocation.
    COMPOSE_PROFILES="*" dc down --remove-orphans "$@"
    # Fails, harmlessly, while the destination stack still uses it.
    docker network rm outpost-dest >/dev/null 2>&1 || true
    ;;
  run)
    # One-off command in a service, e.g. `dev.sh run --rm migrate`.
    check_queue_flags
    docker network create outpost-dest >/dev/null 2>&1 || true
    exec docker compose --env-file .env "${files[@]}" run "$@"
    ;;
  *)
    echo "usage: dev.sh up|down|run [extra args]" >&2
    exit 2
    ;;
esac
