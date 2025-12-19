#!/bin/bash
# Reads LOCAL_DEV_* vars from .env and outputs COMPOSE_PROFILES
# Usage: ./profiles.sh        - outputs profiles based on .env
#        ./profiles.sh --all  - outputs all profiles (for down command)

if [ "$1" = "--all" ]; then
  echo "redis,dragonfly,postgres,clickhouse,rabbitmq,localstack,gcp,redis-commander,pgadmin,tabix"
  exit 0
fi

PROFILES=""

# Cache (Redis default, Dragonfly optional)
if grep -q "^LOCAL_DEV_DRAGONFLY=1" .env 2>/dev/null; then
  PROFILES="dragonfly"
else
  PROFILES="redis"
fi

# Log stores
grep -q "^LOCAL_DEV_POSTGRES=1" .env 2>/dev/null && PROFILES="$PROFILES,postgres"
grep -q "^LOCAL_DEV_CLICKHOUSE=1" .env 2>/dev/null && PROFILES="$PROFILES,clickhouse"

# Message queues
grep -q "^LOCAL_DEV_RABBITMQ=1" .env 2>/dev/null && PROFILES="$PROFILES,rabbitmq"
grep -q "^LOCAL_DEV_LOCALSTACK=1" .env 2>/dev/null && PROFILES="$PROFILES,localstack"
grep -q "^LOCAL_DEV_GCP=1" .env 2>/dev/null && PROFILES="$PROFILES,gcp"

# GUI tools
grep -q "^LOCAL_DEV_REDIS_COMMANDER=" .env 2>/dev/null && PROFILES="$PROFILES,redis-commander"
grep -q "^LOCAL_DEV_PGADMIN=" .env 2>/dev/null && PROFILES="$PROFILES,pgadmin"
grep -q "^LOCAL_DEV_TABIX=" .env 2>/dev/null && PROFILES="$PROFILES,tabix"

echo "$PROFILES"
