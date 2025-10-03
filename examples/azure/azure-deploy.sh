#!/bin/bash
set -e

# Check for required environment files
if [ ! -f .env.outpost ] || [ ! -f .env.runtime ]; then
  echo "Error: .env.outpost and/or .env.runtime not found."
  echo "Please run './dependencies.sh' and './local-deploy.sh' first to generate these files."
  exit 1
fi

# Load environment variables
echo "Loading environment variables from .env.outpost and .env.runtime..."
source .env.outpost && source .env.runtime

# Registering required resource providers
echo "Registering required Azure resource providers..."
az provider register -n Microsoft.App --wait
az provider register -n Microsoft.OperationalInsights --wait

# Create Azure Container Apps Environment
echo "Creating Azure Container Apps environment..."
az containerapp env create \
  --name outpost-environment \
  --resource-group $RESOURCE_GROUP \
  --location $LOCATION

# Prepare environment variables for deployment
echo "Preparing environment variables for deployment..."
ENV_VARS_STRING=$(grep -v '^#' .env.runtime | sed 's/^export //g' | sed 's/"//g' | tr '\n' ' ')

# Deploy api service
echo "Deploying api service..."
az containerapp up \
  --name outpost-api \
  --resource-group $RESOURCE_GROUP \
  --location $LOCATION \
  --environment outpost-environment \
  --image hookdeck/outpost:v0.4.0 \
  --target-port 3333 \
  --ingress external \
  --env-vars "SERVICE=api" $ENV_VARS_STRING

# Deploy delivery service
echo "Deploying delivery service..."
az containerapp up \
  --name outpost-delivery \
  --resource-group $RESOURCE_GROUP \
  --location $LOCATION \
  --environment outpost-environment \
  --image hookdeck/outpost:v0.4.0 \
  --ingress internal \
  --env-vars "SERVICE=delivery" $ENV_VARS_STRING

# Deploy log service
echo "Deploying log service..."
az containerapp up \
  --name outpost-log \
  --resource-group $RESOURCE_GROUP \
  --location $LOCATION \
  --environment outpost-environment \
  --image hookdeck/outpost:v0.4.0 \
  --ingress internal \
  --env-vars "SERVICE=log" $ENV_VARS_STRING

echo "Deployment to Azure Container Apps is complete."
echo ""
echo "--- Validating Deployment ---"
echo ""

echo "Fetching logs for outpost-api..."
az containerapp logs show \
  --name outpost-api \
  --resource-group $RESOURCE_GROUP \
  --tail 20

echo ""
echo "Fetching public URL for outpost-api..."
API_URL=$(az containerapp show \
  --name outpost-api \
  --resource-group $RESOURCE_GROUP \
  --query "properties.configuration.ingress.fqdn" \
  --output tsv)

echo "API URL: https://$API_URL"
echo ""

echo "Checking health endpoint..."
HEALTH_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "https://$API_URL/api/v1/healthz")

if [ "$HEALTH_STATUS" -eq 200 ]; then
  echo "✅ Health check passed with status 200."
  echo ""
  echo "✅ Deployment successful!"
  echo "Your API is available at: https://$API_URL"
  echo "You can now use this URL to interact with your Outpost deployment."
else
  echo "❌ Health check failed with status $HEALTH_STATUS."
  echo "There might be an issue with the deployment. Please check the logs for more details."
  exit 1
fi
