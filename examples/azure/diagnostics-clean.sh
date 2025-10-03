#!/bin/bash

set -euo pipefail

# Argument parsing
RUN_LOCAL=false
RUN_AZURE=false

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

# Required variables
REQUIRED_VARS=(
  API_KEY
  RESOURCE_GROUP
)

echo "🔍 Validating required environment variables..."
for VAR in "${REQUIRED_VARS[@]}"; do
  if [ -z "${!VAR:-}" ]; then
    echo "❌ Missing: $VAR"
    exit 1
  fi
done
echo "✅ All required env vars are set."

# Reusable Cleanup Function
run_cleanup() {
    local base_url=$1
    local env_name=$2
    echo "🧹 Cleaning up $env_name environment at $base_url..."
    TENANT_ID="diagnostics-tenant-x"

    echo "   (Fetching destinations for tenant: $TENANT_ID...)"
    DESTINATION_IDS=$(curl -sf -X GET "$base_url/api/v1/$TENANT_ID/destinations" \
        -H "Authorization: Bearer $API_KEY" | jq -r '.[].id')

    if [ -z "$DESTINATION_IDS" ]; then
        echo "   -> No destinations found for tenant $TENANT_ID."
    else
        for DEST_ID in $DESTINATION_IDS; do
            echo "   (Deleting destination: $DEST_ID...)"
            if ! curl -sf -X DELETE "$base_url/api/v1/$TENANT_ID/destinations/$DEST_ID" -H "Authorization: Bearer $API_KEY" >/dev/null; then
                echo "   -> ❌ Failed to delete destination $DEST_ID."
            else
                echo "   -> ✅ Destination $DEST_ID deleted."
            fi
        done
    fi

    echo "   (Deleting tenant: $TENANT_ID...)"
    if ! curl -sf -X DELETE "$base_url/api/v1/$TENANT_ID" -H "Authorization: Bearer $API_KEY" >/dev/null; then
        echo "   -> ❌ Failed to delete tenant $TENANT_ID."
    else
        echo "   -> ✅ Tenant $TENANT_ID deleted."
    fi
}

# Local Cleanup
if [ "$RUN_LOCAL" = true ]; then
    echo "-------------------------------------"
    echo "🧹 Running LOCAL Cleanup..."
    echo "-------------------------------------"
    run_cleanup "http://localhost:3333" "local"
fi

# Azure Cleanup
if [ "$RUN_AZURE" = true ]; then
    echo "-------------------------------------"
    echo "☁️ Running AZURE Cleanup..."
    echo "-------------------------------------"
    
    if ! command -v az &> /dev/null; then
        echo "   -> ❌ Azure CLI 'az' is not installed. Skipping Azure cleanup."
    else
        AZURE_CONTAINER_APP_NAME="outpost-api"
        echo "   (Fetching Azure Container App URL for '$AZURE_CONTAINER_APP_NAME'...)"
        AZURE_URL=$(az containerapp show --name "$AZURE_CONTAINER_APP_NAME" --resource-group "$RESOURCE_GROUP" --query "properties.configuration.ingress.fqdn" -o tsv)
        if [ -z "$AZURE_URL" ]; then
            echo "   -> ❌ Could not fetch Azure Container App URL for '$AZURE_CONTAINER_APP_NAME'. Check your Azure login and configuration."
        else
            run_cleanup "https://$AZURE_URL" "azure"
        fi
    fi
fi

echo "✅ Cleanup complete."