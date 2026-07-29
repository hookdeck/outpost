#!/bin/bash
set -e

# Default size is small
SIZE="small"

# Parse command-line arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --size=*)
      SIZE="${1#*=}"
      ;;
    --size)
      SIZE="$2"
      shift
      ;;
    *)
      # Unknown option
      ;;
  esac
  shift
done

# Validate size parameter
if [[ "$SIZE" != "small" && "$SIZE" != "medium" ]]; then
  echo "Invalid size: $SIZE. Valid options are 'small' or 'medium'"
  exit 1
fi

# Function to log with timestamp to both stdout and logfile
log() {
    local timestamp="[$(date '+%Y-%m-%d %H:%M:%S')]"
    echo "$timestamp $1" >&3
    echo "$timestamp $1" >> "$LOGFILE"
}

# Function to handle errors
handle_error() {
    local exit_code=$?
    log "❌ Error occurred (exit code: $exit_code). Check logs at: $LOGFILE"
    log "To clean up this environment:
  ./scripts/down.sh $NAMESPACE"
    exit $exit_code
}

# Setup
TIMESTAMP=$(date +%s)
NAMESPACE="outpost-loadtest-$TIMESTAMP"
LOGDIR="logs"
mkdir -p $LOGDIR
LOGFILE="$LOGDIR/outpost-loadtest-$TIMESTAMP.log"

# Save original stdout/stderr
exec 3>&1
exec 4>&2

# Set error handler
trap handle_error ERR

# Check if Minikube is running
if ! minikube status >/dev/null 2>&1; then
    log "❌ Minikube is not running. Please start it with 'minikube start' first."
    exit 1
fi

# Main execution - all output goes to logfile unless explicitly logged to stdout
{
    log "🚀 Starting Outpost load test environment with size: $SIZE..."
    
    # Create and switch to a new namespace
    log "🔄 Creating namespace: $NAMESPACE..."
    kubectl create namespace $NAMESPACE --dry-run=client -o yaml | kubectl apply -f -
    kubectl config set-context --current --namespace=$NAMESPACE

    # Add Bitnami repo if not exists
    if ! helm repo list | grep -q "bitnami"; then
        log "📦 Adding Bitnami Helm repo..."
        helm repo add bitnami https://charts.bitnami.com/bitnami
        helm repo update
    fi

    # Install Redis
    log "🔴 Installing Redis ($SIZE)..."
    helm install outpost-redis bitnami/redis \
        -f values/redis/$SIZE.yaml \
        --namespace $NAMESPACE \
        --wait

    # Verify Redis is running
    kubectl rollout status statefulset/outpost-redis-master -n $NAMESPACE --timeout=300s

    # Get Redis password
    REDIS_PASSWORD=$(kubectl get secret outpost-redis -o jsonpath="{.data.redis-password}" -n $NAMESPACE | base64 -d)

    # Install PostgreSQL
    log "🐘 Installing PostgreSQL ($SIZE)..."
    helm install outpost-postgresql bitnami/postgresql \
        -f values/postgres/$SIZE.yaml \
        --namespace $NAMESPACE \
        --wait

    # Verify PostgreSQL is running
    kubectl rollout status statefulset/outpost-postgresql -n $NAMESPACE --timeout=300s

    # Get PostgreSQL password
    POSTGRES_PASSWORD=$(kubectl get secret outpost-postgresql -o jsonpath="{.data.password}" -n $NAMESPACE | base64 -d)
    POSTGRES_URL="postgresql://outpost:${POSTGRES_PASSWORD}@outpost-postgresql:5432/outpost?sslmode=disable"

    # Install RabbitMQ
    log "🐰 Installing RabbitMQ ($SIZE)..."
    helm install outpost-rabbitmq bitnami/rabbitmq \
        -f values/rabbitmq/$SIZE.yaml \
        --namespace $NAMESPACE \
        --wait

    # Verify RabbitMQ is running
    kubectl rollout status statefulset/outpost-rabbitmq -n $NAMESPACE --timeout=300s

    # Get RabbitMQ password
    RABBITMQ_PASSWORD=$(kubectl get secret outpost-rabbitmq -o jsonpath="{.data.rabbitmq-password}" -n $NAMESPACE | base64 -d)
    RABBITMQ_SERVER_URL="amqp://outpost:${RABBITMQ_PASSWORD}@outpost-rabbitmq:5672"

    # Generate application secrets
    log "🔑 Generating application secrets..."
    API_KEY=$(openssl rand -hex 16)
    API_JWT_SECRET=$(openssl rand -hex 32)
    AES_ENCRYPTION_SECRET=$(openssl rand -hex 32)

    # Create or update Kubernetes secret
    log "🔒 Creating Kubernetes secrets..."
    kubectl create secret generic outpost-secrets \
        --namespace $NAMESPACE \
        --from-literal=POSTGRES_URL="$POSTGRES_URL" \
        --from-literal=REDIS_HOST="outpost-redis-master" \
        --from-literal=REDIS_PASSWORD="$REDIS_PASSWORD" \
        --from-literal=RABBITMQ_SERVER_URL="$RABBITMQ_SERVER_URL" \
        --from-literal=API_KEY="$API_KEY" \
        --from-literal=API_JWT_SECRET="$API_JWT_SECRET" \
        --from-literal=AES_ENCRYPTION_SECRET="$AES_ENCRYPTION_SECRET" \
        --save-config --dry-run=client -o yaml | kubectl apply -f -

    # Install Outpost
    log "🚀 Installing Outpost ($SIZE)..."
    helm install outpost ../../../../deployments/kubernetes/charts/outpost \
        -f values/outpost/$SIZE.yaml \
        --namespace $NAMESPACE \
        --wait

    # Verify Outpost is running
    kubectl rollout status deployment/outpost-api -n $NAMESPACE --timeout=300s

    # Print success message
    log "✅ Setup complete! 

Environment Details:
------------------
Namespace: $NAMESPACE
Size: $SIZE
Log file: $LOGFILE

API Configuration:
----------------
API Key: $API_KEY
API URL: http://localhost:3333 (after port-forward)

Database Connections:
------------------
PostgreSQL:
  Host: outpost-postgresql
  Port: 5432
  Database: outpost
  Username: outpost
  Password: $POSTGRES_PASSWORD
  URL: $POSTGRES_URL
  Connect command: kubectl exec -it -n $NAMESPACE outpost-postgresql-0 -- bash -c 'PGPASSWORD=$POSTGRES_PASSWORD psql -U outpost -d outpost'

Redis:
  Host: outpost-redis-master
  Port: 6379
  Password: $REDIS_PASSWORD
  Connect command: kubectl exec -it -n $NAMESPACE outpost-redis-master-0 -- redis-cli -a $REDIS_PASSWORD

RabbitMQ:
  Host: outpost-rabbitmq
  AMQP Port: 5672
  Management Port: 15672
  Username: outpost
  Password: $RABBITMQ_PASSWORD
  URL: $RABBITMQ_SERVER_URL
  Management URL: http://localhost:15672 (after port-forward)

Useful Commands:
--------------
# Port forward API:
kubectl port-forward svc/outpost 3333:3333 -n ${NAMESPACE}

# Port forward RabbitMQ Management:
kubectl port-forward svc/outpost-rabbitmq 15672:15672 -n ${NAMESPACE}

# PostgreSQL:
# Option 1 - Connect directly via kubectl:
kubectl exec -it -n ${NAMESPACE} outpost-postgresql-0 -- bash -c 'PGPASSWORD=$POSTGRES_PASSWORD psql -U outpost -d outpost'
# Option 2 - Port forward for local GUI tools:
kubectl port-forward svc/outpost-postgresql 5432:5432 -n ${NAMESPACE}

# Redis:
# Option 1 - Connect directly via kubectl:
kubectl exec -it -n ${NAMESPACE} outpost-redis-master-0 -- redis-cli -a ${REDIS_PASSWORD}
# Option 2 - Port forward for local GUI tools:
kubectl port-forward svc/outpost-redis-master 6379:6379 -n ${NAMESPACE}

# View logs:
kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=outpost -f

# View logs by component:
kubectl logs -f -l app.kubernetes.io/name=outpost,app.kubernetes.io/component=api -n $NAMESPACE      # API logs
kubectl logs -f -l app.kubernetes.io/name=outpost,app.kubernetes.io/component=delivery -n $NAMESPACE # Delivery logs
kubectl logs -f -l app.kubernetes.io/name=outpost,app.kubernetes.io/component=log -n $NAMESPACE     # Log service logs

# Clean up:
./scripts/down.sh $NAMESPACE

Example API Calls:
----------------
# First, start port forwarding in a separate terminal:
kubectl port-forward svc/outpost 3333:3333 -n $NAMESPACE

# Your API key: $API_KEY

# Health check
curl http://localhost:3333/api/v1/healthz

# Create tenant
curl -v -X PUT http://localhost:3333/api/v1/tenants/123 \
  -H 'Authorization: Bearer $API_KEY' \
  -H 'Content-Type: application/json'

# Create a destination (replace URL with your endpoint)
WEBHOOK_URL='http://example.com/webhook' curl -v -X POST http://localhost:3333/api/v1/tenants/123/destinations \
  -H 'Authorization: Bearer $API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{
    \"type\": \"webhook\",
    \"topics\": [\"*\"],
    \"config\": {
      \"url\": \"$WEBHOOK_URL\"
    },
    \"credentials\": {}
  }'

# Send an event
curl -v -X POST http://localhost:3333/api/v1/publish \
  -H 'Authorization: Bearer $API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{
    \"tenant_id\": \"123\",
    \"topic\": \"test.event\",
    \"data\": {\"hello\": \"world\"}
  }'

👉 When you are done, clean up the environment with:
./scripts/down.sh $NAMESPACE"

} >> "$LOGFILE" 2>&1 