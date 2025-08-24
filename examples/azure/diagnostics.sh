#!/bin/bash

set -euo pipefail

AZURE_CONTAINER_APP_NAME="outpost-api"

# Argument parsing
RUN_LOCAL=false
RUN_AZURE=false
WEBHOOK_URL_FLAG=""
 
if [ "$#" -eq 0 ]; then
    RUN_LOCAL=true
    RUN_AZURE=true
else
    while [[ "$#" -gt 0 ]]; do
        case $1 in
            --local)
            RUN_LOCAL=true
            shift
            ;;
            --azure)
            RUN_AZURE=true
            shift
            ;;
            --webhook-url)
            if [ -n "$2" ]; then
                WEBHOOK_URL_FLAG="$2"
                shift 2
            else
                echo "Error: --webhook-url requires a non-empty string argument."
                exit 1
            fi
            ;;
            *)
            shift
            ;;
        esac
    done
fi

# Environment files
ENV_FILES=(".env.outpost" ".env.runtime")

# Check and load environment files
for ENV_FILE in "${ENV_FILES[@]}"; do
    if [ ! -f "$ENV_FILE" ]; then
        echo "❌ $ENV_FILE not found. Please run your deploy script first."
        exit 1
    fi
    echo "📄 Loading environment variables from $ENV_FILE..."
    set -a; source "$ENV_FILE"; set +a
done

# 1. Required variables
REQUIRED_VARS=(
  API_KEY
  API_JWT_SECRET
  AES_ENCRYPTION_SECRET
  POSTGRES_URL
  RESOURCE_GROUP
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
    echo "❌ Missing: $VAR"
    exit 1
  fi
done
echo "✅ All required env vars are set."
 
# Webhook URL configuration
if [ -n "$WEBHOOK_URL_FLAG" ]; then
  WEBHOOK_URL="$WEBHOOK_URL_FLAG"
  echo "🔧 Using webhook URL from command line flag."
elif [ -n "${WEBHOOK_URL:-}" ]; then
  echo "🔧 Using webhook URL from environment variable."
else
  echo "❌ Webhook URL is not set. Please provide it via the --webhook-url flag or the WEBHOOK_URL environment variable."
  exit 1
fi

# 2. Host extractions
PG_HOST=$(echo "$POSTGRES_URL" | sed -E 's|.*@([^:/]+):.*|\1|')
SB_FQDN="${AZURE_SERVICEBUS_NAMESPACE}.servicebus.windows.net"

# 3. DNS and port checks
check_port() {
  echo -n "🌐 Testing $1:$2 ... "
  if nc -z -w 5 "$1" "$2"; then
    echo "✅ Reachable"
  else
    echo "❌ Unreachable"
  fi
}

echo "🌐 Testing network connectivity..."
check_port "$PG_HOST" 5432
check_port "$REDIS_HOST" "$REDIS_PORT"
check_port "$SB_FQDN" 443

# 4. Service Bus Permissions Test
echo "🔐 Testing Azure Service Bus permissions..."

if ! command -v jq &> /dev/null; then
    echo "   -> ❌ jq is not installed, which is required for this check. Skipping permissions test."
else
    # Define the two scopes we will check against
    NAMESPACE_SCOPE="/subscriptions/$AZURE_SERVICEBUS_SUBSCRIPTION_ID/resourceGroups/$AZURE_SERVICEBUS_RESOURCE_GROUP/providers/Microsoft.ServiceBus/namespaces/$AZURE_SERVICEBUS_NAMESPACE"
    TOPIC_SCOPE="$NAMESPACE_SCOPE/topics/$AZURE_SERVICEBUS_DELIVERY_TOPIC"

    echo "   (Getting Service Principal Object ID...)"
    # Note: This command relies on the user being logged into the az CLI
    SP_OBJECT_ID=$(az ad sp show --id "$AZURE_SERVICEBUS_CLIENT_ID" --query "id" -o tsv)

    if [ -z "$SP_OBJECT_ID" ]; then
        echo "   -> ❌ Could not retrieve Service Principal Object ID. Please check your Azure login and that the SP exists."
    else
        permission_found=false
        # Function to check for a specific role assignment at a specific scope
        check_role() {
            local role_name=$1
            local scope=$2
            local scope_name=$3 # A friendly name for the scope for logging

            echo "   (Checking for role: '$role_name' at $scope_name scope...)"
            if az role assignment list --assignee "$SP_OBJECT_ID" --scope "$scope" --query "contains([].roleDefinitionName, '$role_name')" | grep -q "true"; then
                echo "   -> ✅ Service principal has the required '$role_name' role at the $scope_name scope."
                permission_found=true
            else
                echo "   -> No '$role_name' role found at $scope_name scope."
            fi
        }

        # 1. Check for Data Owner at the Namespace level (highest privilege)
        check_role "Azure Service Bus Data Owner" "$NAMESPACE_SCOPE" "Namespace"

        # 2. Check for Data Sender at the Namespace level
        check_role "Azure Service Bus Data Sender" "$NAMESPACE_SCOPE" "Namespace"

        # 3. Check for Data Sender at the Topic level (most specific)
        check_role "Azure Service Bus Data Sender" "$TOPIC_SCOPE" "Topic"

        # If none of the checks passed, show a final error
        if [ "$permission_found" = false ]; then
            echo ""
            echo "   -> ❌ PERMISSION FAILURE: The Service Principal does NOT have the required permissions to publish to topic '$AZURE_SERVICEBUS_DELIVERY_TOPIC'."
            echo "      To fix, grant the 'Azure Service Bus Data Sender' role at either the Namespace or the specific Topic scope."
            echo "      (Alternatively, 'Azure Service Bus Data Owner' at the Namespace level also works)."
            echo "      Run one of the following commands:"
            echo "      Namespace Level: az role assignment create --assignee \"$SP_OBJECT_ID\" --role \"Azure Service Bus Data Sender\" --scope \"$NAMESPACE_SCOPE\""
            echo "      Topic Level:     az role assignment create --assignee \"$SP_OBJECT_ID\" --role \"Azure Service Bus Data Sender\" --scope \"$TOPIC_SCOPE\""
        else
            echo "   -> ✅ Permissions are sufficient for publishing."
        fi
    fi
fi

# Reusable API Test Function
run_api_tests() {
    local base_url=$1
    echo "🚀 Testing Outpost API at $base_url..."
    TENANT_ID="diagnostics-tenant-x"
    local event_source="local"
    if [[ "$base_url" == *"azurecontainerapps.io"* ]]; then
        event_source="azure"
    fi

    echo "   (Creating tenant: $TENANT_ID...)"
    if ! curl -sf -X PUT "$base_url/api/v1/$TENANT_ID" -H "Authorization: Bearer $API_KEY" >/dev/null; then
        echo "   -> ❌ Failed to create tenant."
        if [[ "$base_url" == *"azurecontainerapps.io"* ]]; then
            echo "      Fetching logs for '$AZURE_CONTAINER_APP_NAME'..."
            az containerapp logs show --name "$AZURE_CONTAINER_APP_NAME" --resource-group "$RESOURCE_GROUP" --tail 20
        else
            echo "      Check the Outpost API logs."
        fi
        return 1
    fi
    echo "   -> ✅ Tenant created."

    echo "   (Creating webhook destination...)"
    DESTINATION_ID=$(curl -sf -X POST "$base_url/api/v1/$TENANT_ID/destinations" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $API_KEY" \
    -d "{\"type\":\"webhook\",\"topics\":[\"*\"],\"config\":{\"url\":\"$WEBHOOK_URL\"}}" | jq -r .id)

    if [ -z "$DESTINATION_ID" ]; then
        echo "   -> ❌ Failed to create webhook destination."
        if [[ "$base_url" == *"azurecontainerapps.io"* ]]; then
            echo "      Fetching logs for '$AZURE_CONTAINER_APP_NAME'..."
            az containerapp logs show --name "$AZURE_CONTAINER_APP_NAME" --resource-group "$RESOURCE_GROUP" --tail 20
        fi
        return 1
    fi
    echo "   -> ✅ Webhook destination created."

    echo "   (Publishing test event...)"
    if ! curl -sf -X POST "$base_url/api/v1/publish" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $API_KEY" \
    -d "{\"tenant_id\":\"$TENANT_ID\",\"topic\":\"diagnostics.test\",\"data\":{\"hello\":\"world\",\"source\":\"$event_source\"}}" >/dev/null; then
        echo "   -> ❌ Failed to publish event."
        if [[ "$base_url" == *"azurecontainerapps.io"* ]]; then
            echo "      Fetching logs for '$AZURE_CONTAINER_APP_NAME'..."
            az containerapp logs show --name "$AZURE_CONTAINER_APP_NAME" --resource-group "$RESOURCE_GROUP" --tail 20
        fi
        return 1
    fi
    echo "   -> ✅ Event published."

    echo "   (Getting Outpost portal URL...)"
    PORTAL_URL=$(curl -sf "$base_url/api/v1/$TENANT_ID/portal" -H "Authorization: Bearer $API_KEY" | jq -r .redirect_url)
    if [ -z "$PORTAL_URL" ]; then
        echo "   -> ⚠️  Could not retrieve portal URL."
    else
        echo "   -> ✅ View event details at: $PORTAL_URL"
    fi

    echo "   (Testing destination deletion...)"
    if ! curl -sf -X DELETE "$base_url/api/v1/$TENANT_ID/destinations/$DESTINATION_ID" \
    -H "Authorization: Bearer $API_KEY" >/dev/null; then
        echo "   -> ❌ Failed to delete webhook destination."
        if [[ "$base_url" == *"azurecontainerapps.io"* ]]; then
            echo "      Fetching logs for '$AZURE_CONTAINER_APP_NAME'..."
            az containerapp logs show --name "$AZURE_CONTAINER_APP_NAME" --resource-group "$RESOURCE_GROUP" --tail 20
        fi
        return 1
    fi
    echo "   -> ✅ Webhook destination deleted."
}

# 5. Local Deployment Tests
if [ "$RUN_LOCAL" = true ]; then
    echo "-------------------------------------"
    echo "🩺 Running LOCAL Deployment Tests..."
    echo "-------------------------------------"

    # Postgres test
    echo "🐘 Testing PostgreSQL login..."
    docker run -i --rm postgres psql "$POSTGRES_URL" -c '\l' >/dev/null 2>&1 && \
    echo "✅ PostgreSQL login successful" || \
    echo "❌ PostgreSQL login failed"

    # Redis test (Azure Managed Redis uses TLS on port 10000 by default)
    echo "🧪 Testing Azure Managed Redis connection on port $REDIS_PORT..."
    if [ "${REDIS_TLS_ENABLED:-false}" = "true" ]; then
        echo "   -> Testing with TLS encryption (skipping cert verification for Azure Managed Redis)..."
        if docker run -i --rm redis redis-cli --tls --insecure -h "$REDIS_HOST" -p "$REDIS_PORT" -a "$REDIS_PASSWORD" -n "$REDIS_DATABASE" ping 2>/dev/null; then
            echo "✅ Azure Managed Redis responded to ping with TLS on port $REDIS_PORT"
        else
            echo "❌ Azure Managed Redis TLS connection failed on port $REDIS_PORT. Trying fallback test..."
            # Fallback: Try with certificate files if they exist
            if docker run -i --rm redis redis-cli --tls --cert /etc/ssl/certs/redis.crt --key /etc/ssl/private/redis.key --cacert /etc/ssl/certs/redis.ca -h "$REDIS_HOST" -p "$REDIS_PORT" -a "$REDIS_PASSWORD" -n "$REDIS_DATABASE" ping 2>/dev/null; then
                echo "✅ Azure Managed Redis responded to ping with certificate validation"
            else
                echo "❌ Azure Managed Redis connection failed with both insecure and certificate modes"
            fi
        fi
    else
        echo "   -> Testing without TLS..."
        if docker run -i --rm redis redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" -a "$REDIS_PASSWORD" -n "$REDIS_DATABASE" ping; then
            echo "✅ Azure Managed Redis responded to ping on port $REDIS_PORT"
        else
            echo "❌ Azure Managed Redis connection failed on port $REDIS_PORT. See error above."
        fi
    fi

    # API Test
    run_api_tests "http://localhost:3333"
fi

# 6. Azure Deployment Tests
if [ "$RUN_AZURE" = true ]; then
    echo "-------------------------------------"
    echo "☁️ Running AZURE Deployment Tests..."
    echo "-------------------------------------"

    if ! command -v az &> /dev/null; then
        echo "   -> ❌ Azure CLI 'az' is not installed. Skipping Azure tests."
    else
        echo "   (Fetching Azure Container App URL for '$AZURE_CONTAINER_APP_NAME'...)"
        AZURE_URL=$(az containerapp show --name "$AZURE_CONTAINER_APP_NAME" --resource-group "$RESOURCE_GROUP" --query "properties.configuration.ingress.fqdn" -o tsv)
        if [ -z "$AZURE_URL" ]; then
            echo "   -> ❌ Could not fetch Azure Container App URL for '$AZURE_CONTAINER_APP_NAME'. Check your Azure login and configuration."
        else
            # API Test
            run_api_tests "https://$AZURE_URL"
        fi
    fi
fi

# 7. Time check
echo "⏱️ Checking system time sync..."
docker run -i --rm busybox date
date

echo "🔍 Diagnostics complete."
