#!/bin/bash

# Azure dependencies setup script
# This script creates Azure resources including PostgreSQL, Azure Managed Redis, and Service Bus
# Note: Uses Azure Managed Redis (az redisenterprise) instead of Azure Cache for Redis (az redis)
# for better performance, features, and cost-effectiveness
# Requires: Azure CLI with redisenterprise extension (auto-installed by this script)

set -euo pipefail

# Set PG_PASS from env var, extract from existing .env file, or generate a new one
if [[ -z "${PG_PASS-}" ]]; then
  # Try to extract password from existing .env.outpost file
  if [[ -f ".env.outpost" ]] && grep -q "POSTGRES_URL" .env.outpost; then
    echo "🔍 Extracting existing PostgreSQL password from .env.outpost..."
    PG_PASS=$(grep POSTGRES_URL .env.outpost | sed 's/.*:\/\/[^:]*:\([^@]*\)@.*/\1/')
    echo "🔑 Using existing PostgreSQL password"
  else
    echo "🔑 Generating new PostgreSQL password..."
    # Generate a random alphanumeric password of 24 characters
    PG_PASS=$(openssl rand -base64 32 | tr -dc 'a-zA-Z0-9' | head -c 24)
  fi
fi

# CONFIG
ENV_FILE=".env.outpost"
LOCATION="westeurope"
RESOURCE_GROUP="outpost-azure"
PG_NAME="outpost-pg-example"
PG_DB_NAME="outpost"
PG_ADMIN="outpostadmin"
REDIS_NAME="outpost-redis"
SB_NAMESPACE="outpost-internal"
SB_DELIVERY_TOPIC="outpost-delivery"
SB_DELIVERY_SUB="outpost-delivery-sub"
SB_LOG_TOPIC="outpost-log"
SB_LOG_SUB="outpost-log-sub"

# Load previous env if available
if [[ -f "$ENV_FILE" ]]; then
  echo "📄 Loading existing $ENV_FILE..."
  set -a; source "$ENV_FILE"; set +a
fi

# Create resource group
echo "🔎 Checking if resource group '$RESOURCE_GROUP' exists..."
if ! az group show --name "$RESOURCE_GROUP" &>/dev/null; then
  echo "📦 Creating resource group..."
  az group create --name "$RESOURCE_GROUP" --location "$LOCATION" >/dev/null
else
  echo "✅ Resource group exists"
fi

# Register PostgreSQL provider
echo "🔎 Checking if Microsoft.DBforPostgreSQL is registered..."
if ! az provider show --namespace Microsoft.DBforPostgreSQL --query "registrationState" -o tsv | grep -q "Registered"; then
  echo "📥 Registering Microsoft.DBforPostgreSQL..."
  az provider register --namespace Microsoft.DBforPostgreSQL >/dev/null
  echo "⏳ Waiting for registration to complete..."
  until az provider show --namespace Microsoft.DBforPostgreSQL --query "registrationState" -o tsv | grep -q "Registered"; do sleep 3; done
fi
echo "✅ PostgreSQL provider registered"

# Create PostgreSQL server
echo "🔎 Checking if PostgreSQL server '$PG_NAME' exists..."
if ! az postgres flexible-server show --name "$PG_NAME" --resource-group "$RESOURCE_GROUP" &>/dev/null; then
  echo "🐘 Creating PostgreSQL Flexible Server..."
  az postgres flexible-server create \
    --name "$PG_NAME" \
    --resource-group "$RESOURCE_GROUP" \
    --location "$LOCATION" \
    --admin-user "$PG_ADMIN" \
    --admin-password "$PG_PASS" \
    --sku-name Standard_B1ms \
    --tier "Burstable" \
    --public-access 0.0.0.0-255.255.255.255 \
    --yes
else
  echo "✅ PostgreSQL server already exists"
fi

# Create PostgreSQL database
echo "🔎 Checking if database '$PG_DB_NAME' exists..."
if ! az postgres flexible-server db show --database-name "$PG_DB_NAME" --server-name "$PG_NAME" --resource-group "$RESOURCE_GROUP" &>/dev/null; then
  echo "📦 Creating database '$PG_DB_NAME'..."
  az postgres flexible-server db create --database-name "$PG_DB_NAME" --server-name "$PG_NAME" --resource-group "$RESOURCE_GROUP"
else
  echo "✅ PostgreSQL database already exists"
fi

# Register Redis Enterprise provider (for Azure Managed Redis)
echo "🔎 Checking if Microsoft.Cache is registered..."
if ! az provider show --namespace Microsoft.Cache --query "registrationState" -o tsv | grep -q "Registered"; then
  echo "📥 Registering Microsoft.Cache..."
  az provider register --namespace Microsoft.Cache >/dev/null
  echo "⏳ Waiting for registration to complete..."
  until az provider show --namespace Microsoft.Cache --query "registrationState" -o tsv | grep -q "Registered"; do sleep 3; done
fi

# Install Azure CLI Redis Enterprise extension
echo "🔧 Installing Azure CLI Redis Enterprise extension..."
if ! az extension list --query "[?name=='redisenterprise'].name" -o tsv | grep -q "redisenterprise"; then
  echo "📥 Installing redisenterprise extension..."
  az extension add --name redisenterprise --yes >/dev/null 2>&1 || {
    echo "⚠️  Extension installation may require confirmation. Enabling dynamic install..."
    az config set extension.dynamic_install_allow_preview=true
    az extension add --name redisenterprise --yes >/dev/null 2>&1
  }
else
  echo "✅ Redis Enterprise extension already installed"
fi

# Create Azure Managed Redis cluster
echo "🔎 Checking if Azure Managed Redis cluster '$REDIS_NAME' exists..."
if ! az redisenterprise show --cluster-name "$REDIS_NAME" --resource-group "$RESOURCE_GROUP" &>/dev/null; then
  echo "🔴 Creating Azure Managed Redis cluster..."
  az redisenterprise create \
    --cluster-name "$REDIS_NAME" \
    --resource-group "$RESOURCE_GROUP" \
    --location "$LOCATION" \
    --sku Enterprise_E1 \
    --minimum-tls-version "1.2"
  
  echo "⏳ Waiting for cluster to be ready..."
  az redisenterprise wait --cluster-name "$REDIS_NAME" --resource-group "$RESOURCE_GROUP" --created
else
  echo "✅ Azure Managed Redis cluster already exists"
fi


# Register ServiceBus provider
echo "🔎 Checking if Microsoft.ServiceBus is registered..."
if ! az provider show --namespace Microsoft.ServiceBus --query "registrationState" -o tsv | grep -q "Registered"; then
  echo "📥 Registering Microsoft.ServiceBus..."
  az provider register --namespace Microsoft.ServiceBus >/dev/null
  echo "⏳ Waiting for registration to complete..."
  until az provider show --namespace Microsoft.ServiceBus --query "registrationState" -o tsv | grep -q "Registered"; do sleep 3; done
fi

# Create Service Bus namespace
echo "📡 Checking if Service Bus namespace '$SB_NAMESPACE' exists..."
if ! az servicebus namespace show --name "$SB_NAMESPACE" --resource-group "$RESOURCE_GROUP" &>/dev/null; then
  echo "📡 Creating Service Bus namespace..."
  az servicebus namespace create --name "$SB_NAMESPACE" --resource-group "$RESOURCE_GROUP" --location "$LOCATION" >/dev/null
  echo "⏳ Waiting for namespace to be ready..."
  az servicebus namespace wait --name "$SB_NAMESPACE" --resource-group "$RESOURCE_GROUP" --created >/dev/null
fi

# Create topics and subscriptions
create_topic_and_sub() {
  local topic=$1
  local sub=$2
  local retries=3
  local delay=5

  # Create Topic
  if ! az servicebus topic show --name "$topic" --namespace-name "$SB_NAMESPACE" --resource-group "$RESOURCE_GROUP" &>/dev/null; then
    echo "📨 Creating topic '$topic'..."
    for i in $(seq 1 $retries); do
      if az servicebus topic create --name "$topic" --namespace-name "$SB_NAMESPACE" --resource-group "$RESOURCE_GROUP" --max-size 1024 >/dev/null 2>&1; then
        break
      fi
      if [ $i -lt $retries ]; then
        echo "Attempt $i failed. Retrying in $delay seconds..."
        sleep $delay
      else
        echo "❌ Failed to create topic '$topic' after $retries attempts."
        exit 1
      fi
    done
  else
    echo "✅ Topic '$topic' already exists"
  fi

  # Create Subscription
  if ! az servicebus topic subscription show --name "$sub" --topic-name "$topic" --namespace-name "$SB_NAMESPACE" --resource-group "$RESOURCE_GROUP" &>/dev/null; then
    echo "🔔 Creating subscription '$sub' for topic '$topic'..."
    for i in $(seq 1 $retries); do
      if az servicebus topic subscription create --name "$sub" --topic-name "$topic" --namespace-name "$SB_NAMESPACE" --resource-group "$RESOURCE_GROUP" >/dev/null 2>&1; then
        break
      fi
      if [ $i -lt $retries ]; then
        echo "Attempt $i failed. Retrying in $delay seconds..."
        sleep $delay
      else
        echo "❌ Failed to create subscription '$sub' after $retries attempts."
        exit 1
      fi
    done
  else
    echo "✅ Subscription '$sub' already exists"
  fi
}

create_topic_and_sub "$SB_DELIVERY_TOPIC" "$SB_DELIVERY_SUB"
echo "⏳ Pausing for 5 seconds before creating next topic..."
sleep 5
create_topic_and_sub "$SB_LOG_TOPIC" "$SB_LOG_SUB"

# Create service principal for Service Bus access
echo "👤 Creating or updating service principal for Outpost access..."
sp_name="outpost-sp-$(echo -n "$RESOURCE_GROUP" | md5sum | cut -c1-10)"
sp_info=$(az ad sp create-for-rbac --name "$sp_name" --sdk-auth)

CLIENT_ID=$(echo "$sp_info" | jq -r .clientId)
CLIENT_SECRET=$(echo "$sp_info" | jq -r .clientSecret)
TENANT_ID=$(echo "$sp_info" | jq -r .tenantId)
SUBSCRIPTION_ID=$(az account show --query id -o tsv)

echo "🔐 Assigning Service Bus roles..."
SP_OBJECT_ID=$(az ad sp show --id "$CLIENT_ID" --query "id" -o tsv)
SCOPE="/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.ServiceBus/namespaces/$SB_NAMESPACE"

assign_and_verify_role() {
  local role_name=$1
  echo "   -> Assigning '$role_name'..."
  az role assignment create \
    --assignee-object-id "$SP_OBJECT_ID" \
    --assignee-principal-type "ServicePrincipal" \
    --role "$role_name" \
    --scope "$SCOPE" >/dev/null

  echo "   -> Waiting for '$role_name' to propagate..."
  for i in {1..10}; do
    if az role assignment list --assignee "$SP_OBJECT_ID" --scope "$SCOPE" --query "contains([].roleDefinitionName, '$role_name')" | grep -q "true"; then
      echo "   -> ✅ '$role_name' confirmed."
      return 0
    fi
    sleep 15
  done
  echo "   -> ❌ '$role_name' could not be confirmed."
  exit 1
}

assign_and_verify_role "Azure Service Bus Data Owner"

# Get Redis host and password for Azure Managed Redis
REDIS_HOST=$(az redisenterprise show --cluster-name "$REDIS_NAME" --resource-group "$RESOURCE_GROUP" --query hostName -o tsv)
REDIS_KEYS=$(az redisenterprise database list-keys --cluster-name "$REDIS_NAME" --resource-group "$RESOURCE_GROUP")
REDIS_PASSWORD=$(echo "$REDIS_KEYS" | jq -r .primaryKey)

# Build .env.outpost file
cat > "$ENV_FILE" <<EOF
LOCATION=$LOCATION
RESOURCE_GROUP=$RESOURCE_GROUP
POSTGRES_URL=postgres://$PG_ADMIN:$PG_PASS@$PG_NAME.postgres.database.azure.com:5432/$PG_DB_NAME?sslmode=require
REDIS_HOST=$REDIS_HOST
REDIS_PORT=10000
REDIS_PASSWORD=$REDIS_PASSWORD
REDIS_DATABASE=0
REDIS_TLS_ENABLED=true
AZURE_SERVICEBUS_CLIENT_ID=$CLIENT_ID
AZURE_SERVICEBUS_CLIENT_SECRET=$CLIENT_SECRET
AZURE_SERVICEBUS_SUBSCRIPTION_ID=$SUBSCRIPTION_ID
AZURE_SERVICEBUS_TENANT_ID=$TENANT_ID
AZURE_SERVICEBUS_NAMESPACE=$SB_NAMESPACE
AZURE_SERVICEBUS_RESOURCE_GROUP=$RESOURCE_GROUP
AZURE_SERVICEBUS_DELIVERY_TOPIC=$SB_DELIVERY_TOPIC
AZURE_SERVICEBUS_DELIVERY_SUBSCRIPTION=$SB_DELIVERY_SUB
AZURE_SERVICEBUS_LOG_TOPIC=$SB_LOG_TOPIC
AZURE_SERVICEBUS_LOG_SUBSCRIPTION=$SB_LOG_SUB
EOF

echo "✅ Done. Values written to $ENV_FILE"
