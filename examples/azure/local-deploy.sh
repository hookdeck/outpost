#!/bin/bash

set -euo pipefail

ENV_FILE=".env.outpost"
RUNTIME_ENV=".env.runtime"

if [ ! -f "$ENV_FILE" ]; then
  echo "❌ $ENV_FILE not found. Please run ./dependencies.sh first."
  exit 1
fi

echo "📄 Loading environment variables from $ENV_FILE..."
set -a; source "$ENV_FILE"; set +a

REQUIRED_VARS=(
  POSTGRES_URL
  REDIS_HOST
  REDIS_PORT
  REDIS_PASSWORD
  REDIS_DATABASE
  AZURE_SERVICEBUS_CLIENT_ID
  AZURE_SERVICEBUS_CLIENT_SECRET
  AZURE_SERVICEBUS_SUBSCRIPTION_ID
  AZURE_SERVICEBUS_TENANT_ID
  AZURE_SERVICEBUS_NAMESPACE
  AZURE_SERVICEBUS_RESOURCE_GROUP
  AZURE_SERVICEBUS_DELIVERY_TOPIC
  AZURE_SERVICEBUS_DELIVERY_SUBSCRIPTION
  AZURE_SERVICEBUS_LOG_TOPIC
  AZURE_SERVICEBUS_LOG_SUBSCRIPTION
)

echo "🔍 Validating required environment variables..."
for VAR in "${REQUIRED_VARS[@]}"; do
  if [ -z "${!VAR:-}" ]; then
    echo "❌ Missing required env var: $VAR"
    exit 1
  fi
done

if [ -s "$RUNTIME_ENV" ]; then
  echo "ℹ️ $RUNTIME_ENV already exists and is not empty. Skipping generation."
else
  echo "⚙️ Writing runtime environment to $RUNTIME_ENV..."

  # Source the latest credentials
  set -a; source "$ENV_FILE"; set +a

  # Generate random values for secrets
  API_KEY_VALUE=$(openssl rand -hex 32)
  API_JWT_SECRET_VALUE=$(openssl rand -hex 32)
  AES_ENCRYPTION_SECRET_VALUE=$(openssl rand -hex 32)

  cat > "$RUNTIME_ENV" <<EOF
# Required
API_KEY="$API_KEY_VALUE"
API_JWT_SECRET="$API_JWT_SECRET_VALUE"
AES_ENCRYPTION_SECRET="$AES_ENCRYPTION_SECRET_VALUE"

# Not required, but recommended
# TOPICS=diagnostics.test,order.created,order.updated,order.deleted

# Required for Postgres logging
POSTGRES_URL=$POSTGRES_URL

# Redis
REDIS_HOST=$REDIS_HOST
REDIS_PORT=$REDIS_PORT
REDIS_PASSWORD=$REDIS_PASSWORD
REDIS_DATABASE=$REDIS_DATABASE

# Azure Service Bus
AZURE_SERVICEBUS_CLIENT_ID=$AZURE_SERVICEBUS_CLIENT_ID
AZURE_SERVICEBUS_CLIENT_SECRET=$AZURE_SERVICEBUS_CLIENT_SECRET
AZURE_SERVICEBUS_SUBSCRIPTION_ID=$AZURE_SERVICEBUS_SUBSCRIPTION_ID
AZURE_SERVICEBUS_TENANT_ID=$AZURE_SERVICEBUS_TENANT_ID
AZURE_SERVICEBUS_NAMESPACE=$AZURE_SERVICEBUS_NAMESPACE
AZURE_SERVICEBUS_RESOURCE_GROUP=$AZURE_SERVICEBUS_RESOURCE_GROUP
AZURE_SERVICEBUS_DELIVERY_TOPIC=$AZURE_SERVICEBUS_DELIVERY_TOPIC
AZURE_SERVICEBUS_DELIVERY_SUBSCRIPTION=$AZURE_SERVICEBUS_DELIVERY_SUBSCRIPTION
AZURE_SERVICEBUS_LOG_TOPIC=$AZURE_SERVICEBUS_LOG_TOPIC
AZURE_SERVICEBUS_LOG_SUBSCRIPTION=$AZURE_SERVICEBUS_LOG_SUBSCRIPTION
EOF
fi

echo "🐳 Launching Outpost services via Docker Compose..."
docker-compose --env-file "$RUNTIME_ENV" up -d --build

echo "✅ Outpost deployed. API should be available on http://localhost:3333"
