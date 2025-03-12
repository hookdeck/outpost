#!/bin/bash
set -e  # Exit on any error

echo "🚀 Setting up Outpost dependencies..."

# Helper function to check if helm release exists
helm_release_exists() {
    helm list | grep -q "^$1"
}

# Helper function to check if secret exists
secret_exists() {
    kubectl get secret "$1" >/dev/null 2>&1
}

# Add Bitnami repo if not exists
if ! helm repo list | grep -q "bitnami"; then
    echo "📦 Adding Bitnami Helm repo..."
    helm repo add bitnami https://charts.bitnami.com/bitnami >/dev/null
    helm repo update >/dev/null
fi

# Install PostgreSQL with custom config
if ! helm_release_exists "outpost-postgresql"; then
    echo "🐘 Installing PostgreSQL..."
    helm install outpost-postgresql bitnami/postgresql \
        --set auth.username=outpost \
        --set auth.database=outpost \
        --set auth.postgresPassword="" >/dev/null
else
    echo "🐘 PostgreSQL already installed, skipping..."
fi
echo "⏳ Waiting for PostgreSQL to be ready..."
kubectl wait --for=condition=ready pod/outpost-postgresql-0 --timeout=120s >/dev/null 2>&1
POSTGRES_PASSWORD=$(kubectl get secret outpost-postgresql -o jsonpath="{.data.password}" | base64 -d)
POSTGRES_URL="postgresql://outpost:${POSTGRES_PASSWORD}@outpost-postgresql:5432/outpost?sslmode=disable"

# Install Redis
if ! helm_release_exists "outpost-redis"; then
    echo "🔴 Installing Redis..."
    helm install outpost-redis bitnami/redis >/dev/null
else
    echo "🔴 Redis already installed, skipping..."
fi
echo "⏳ Waiting for Redis to be ready..."
kubectl wait --for=condition=ready pod/outpost-redis-master-0 --timeout=120s >/dev/null 2>&1
REDIS_PASSWORD=$(kubectl get secret outpost-redis -o jsonpath="{.data.redis-password}" | base64 -d)

# Install RabbitMQ with custom config
if ! helm_release_exists "outpost-rabbitmq"; then
    echo "🐰 Installing RabbitMQ..."
    helm install outpost-rabbitmq bitnami/rabbitmq \
        --set auth.username=outpost \
        --set auth.password="" >/dev/null
else
    echo "🐰 RabbitMQ already installed, skipping..."
fi
echo "⏳ Waiting for RabbitMQ to be ready..."
kubectl wait --for=condition=ready pod/outpost-rabbitmq-0 --timeout=120s >/dev/null 2>&1
RABBITMQ_PASSWORD=$(kubectl get secret outpost-rabbitmq -o jsonpath="{.data.rabbitmq-password}" | base64 -d)
RABBITMQ_SERVER_URL="amqp://outpost:${RABBITMQ_PASSWORD}@outpost-rabbitmq:5672"

# Generate application secrets
echo "🔑 Generating application secrets..."
API_KEY=$(openssl rand -hex 16)
API_JWT_SECRET=$(openssl rand -hex 32)
AES_ENCRYPTION_SECRET=$(openssl rand -hex 32)

# Create or update Kubernetes secret
echo "🔒 Creating/updating Kubernetes secrets..."
kubectl create secret generic outpost-secrets \
    --from-literal=POSTGRES_URL="$POSTGRES_URL" \
    --from-literal=REDIS_HOST="outpost-redis-master" \
    --from-literal=REDIS_PASSWORD="$REDIS_PASSWORD" \
    --from-literal=RABBITMQ_SERVER_URL="$RABBITMQ_SERVER_URL" \
    --from-literal=API_KEY="$API_KEY" \
    --from-literal=API_JWT_SECRET="$API_JWT_SECRET" \
    --from-literal=AES_ENCRYPTION_SECRET="$AES_ENCRYPTION_SECRET" \
    --save-config --dry-run=client -o yaml | kubectl apply -f - >/dev/null

echo "✅ Setup complete! Secrets stored in 'outpost-secrets'

API Key for testing: $API_KEY

Verify your secrets:
   kubectl get secret outpost-secrets                  # Check secret exists
   kubectl get secret outpost-secrets -o yaml          # View encrypted secret
   kubectl get secret outpost-secrets -o jsonpath='{.data.POSTGRES_URL}' | base64 -d    # Verify PostgreSQL URL
   kubectl get secret outpost-secrets -o jsonpath='{.data.REDIS_PASSWORD}' | base64 -d  # Verify Redis password

Install Outpost with:
   helm install outpost ../../deployments/kubernetes/charts/outpost