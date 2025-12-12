#!/bin/bash
set -e  # Exit on any error

echo "🚀 Setting up Outpost dependencies..."

# Helper function to check if pod is healthy
pod_is_healthy() {
    local pod_name=$1
    if kubectl get pod "$pod_name" >/dev/null 2>&1; then
        local status=$(kubectl get pod "$pod_name" -o jsonpath='{.status.phase}')
        local ready=$(kubectl get pod "$pod_name" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')
        
        # Check init containers for ERROR states only (not normal waiting states like "PodInitializing")
        local init_container_status=$(kubectl get pod "$pod_name" -o jsonpath='{.status.initContainerStatuses[*].state.waiting.reason}' 2>/dev/null)
        if [[ "$init_container_status" =~ (ImagePullBackOff|ErrImagePull|CrashLoopBackOff) ]]; then
            return 1
        fi
        
        # Check main containers for error states
        local container_status=$(kubectl get pod "$pod_name" -o jsonpath='{.status.containerStatuses[0].state}' 2>/dev/null)
        if echo "$container_status" | grep -q "waiting"; then
            local waiting_reason=$(kubectl get pod "$pod_name" -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}')
            if [[ "$waiting_reason" =~ (ImagePullBackOff|ErrImagePull|CrashLoopBackOff) ]]; then
                return 1
            fi
        fi
        
        [[ "$status" == "Running" && "$ready" == "True" ]]
    else
        return 1
    fi
}

# Helper function to wait for pod with periodic error checking
wait_for_pod_ready() {
    local pod_name=$1
    local timeout=$2
    local elapsed=0
    local check_interval=5
    
    while [ $elapsed -lt $timeout ]; do
        # Check if pod is ready
        if kubectl wait --for=condition=ready "pod/$pod_name" --timeout=${check_interval}s 2>/dev/null; then
            return 0
        fi
        
        # Check for actual error states only (not just "not ready yet")
        if kubectl get pod "$pod_name" >/dev/null 2>&1; then
            local init_container_status=$(kubectl get pod "$pod_name" -o jsonpath='{.status.initContainerStatuses[*].state.waiting.reason}' 2>/dev/null)
            if [[ "$init_container_status" =~ (ImagePullBackOff|ErrImagePull|CrashLoopBackOff) ]]; then
                return 1
            fi
            
            local container_status=$(kubectl get pod "$pod_name" -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}' 2>/dev/null)
            if [[ "$container_status" =~ (ImagePullBackOff|ErrImagePull|CrashLoopBackOff) ]]; then
                return 1
            fi
        fi
        
        elapsed=$((elapsed + check_interval))
    done
    
    return 1
}

echo "🚀 Using official Docker images (Bitnami images are not available)"
echo ""

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Install PostgreSQL using direct Kubernetes manifests
if ! kubectl get statefulset outpost-postgresql >/dev/null 2>&1; then
    echo "🐘 Installing PostgreSQL (using official postgres:16-alpine image)..."
    
    # Generate a random password
    POSTGRES_PASSWORD=$(openssl rand -hex 16)
    
    # Create PostgreSQL secret
    kubectl create secret generic outpost-postgresql \
        --from-literal=password="$POSTGRES_PASSWORD" \
        --dry-run=client -o yaml | kubectl apply -f - >/dev/null
    
    # Apply PostgreSQL manifests
    kubectl apply -f "$SCRIPT_DIR/postgresql.yaml" >/dev/null
    
    echo "⏳ Waiting for PostgreSQL to be ready (timeout: 120s)..."
    if ! wait_for_pod_ready "outpost-postgresql-0" 120; then
        echo ""
        echo "❌ PostgreSQL pod failed to become ready!"
        echo ""
        echo "Pod status:"
        kubectl get pod outpost-postgresql-0
        echo ""
        echo "Recent events:"
        kubectl describe pod outpost-postgresql-0 | grep -A 10 "Events:" || true
        echo ""
        echo "Container logs (last 20 lines):"
        kubectl logs outpost-postgresql-0 --tail=20 2>/dev/null || echo "  (no logs available)"
        echo ""
        echo "⚠️  To fix this issue, clean up and re-run:"
        echo "     kubectl delete -f $SCRIPT_DIR/postgresql.yaml"
        echo "     kubectl delete pvc data-outpost-postgresql-0"
        echo "     kubectl delete secret outpost-postgresql"
        echo ""
        exit 1
    fi
elif ! pod_is_healthy "outpost-postgresql-0"; then
    echo "❌ PostgreSQL pod is unhealthy!"
    echo ""
    echo "Pod status:"
    kubectl get pod outpost-postgresql-0
    echo ""
    echo "Recent events:"
    kubectl describe pod outpost-postgresql-0 | grep -A 10 "Events:" || true
    echo ""
    echo "Container logs (last 20 lines):"
    kubectl logs outpost-postgresql-0 --tail=20 2>/dev/null || echo "  (no logs available)"
    echo ""
    echo "⚠️  To fix this issue, clean up and re-run:"
    echo "     kubectl delete -f $SCRIPT_DIR/postgresql.yaml"
    echo "     kubectl delete pvc data-outpost-postgresql-0"
    echo "     kubectl delete secret outpost-postgresql"
    echo ""
    exit 1
else
    echo "🐘 PostgreSQL already installed and healthy, skipping..."
fi
POSTGRES_PASSWORD=$(kubectl get secret outpost-postgresql -o jsonpath="{.data.password}" | base64 -d)
POSTGRES_URL="postgresql://outpost:${POSTGRES_PASSWORD}@outpost-postgresql:5432/outpost?sslmode=disable"

# Install Redis using direct Kubernetes manifests
if ! kubectl get statefulset outpost-redis >/dev/null 2>&1; then
    echo "🔴 Installing Redis (using official redis:7-alpine image)..."
    
    # Generate a random password
    REDIS_PASSWORD=$(openssl rand -hex 16)
    
    # Create Redis secret
    kubectl create secret generic outpost-redis \
        --from-literal=redis-password="$REDIS_PASSWORD" \
        --dry-run=client -o yaml | kubectl apply -f - >/dev/null
    
    # Apply Redis manifests
    kubectl apply -f "$SCRIPT_DIR/redis.yaml" >/dev/null
    
    echo "⏳ Waiting for Redis to be ready (timeout: 120s)..."
    if ! wait_for_pod_ready "outpost-redis-0" 120; then
        echo ""
        echo "❌ Redis pod failed to become ready!"
        echo ""
        echo "Pod status:"
        kubectl get pod outpost-redis-0
        echo ""
        echo "Recent events:"
        kubectl describe pod outpost-redis-0 | grep -A 10 "Events:" || true
        echo ""
        echo "Container logs (last 20 lines):"
        kubectl logs outpost-redis-0 --tail=20 2>/dev/null || echo "  (no logs available)"
        echo ""
        echo "⚠️  To fix this issue, clean up and re-run:"
        echo "     kubectl delete -f $SCRIPT_DIR/redis.yaml"
        echo "     kubectl delete pvc data-outpost-redis-0"
        echo "     kubectl delete secret outpost-redis"
        echo ""
        exit 1
    fi
elif ! pod_is_healthy "outpost-redis-0"; then
    echo "❌ Redis pod is unhealthy!"
    echo ""
    echo "Pod status:"
    kubectl get pod outpost-redis-0
    echo ""
    echo "Recent events:"
    kubectl describe pod outpost-redis-0 | grep -A 10 "Events:" || true
    echo ""
    echo "Container logs (last 20 lines):"
    kubectl logs outpost-redis-0 --tail=20 2>/dev/null || echo "  (no logs available)"
    echo ""
    echo "⚠️  To fix this issue, clean up and re-run:"
    echo "     kubectl delete -f $SCRIPT_DIR/redis.yaml"
    echo "     kubectl delete pvc data-outpost-redis-0"
    echo "     kubectl delete secret outpost-redis"
    echo ""
    exit 1
else
    echo "🔴 Redis already installed and healthy, skipping..."
fi
REDIS_PASSWORD=$(kubectl get secret outpost-redis -o jsonpath="{.data.redis-password}" | base64 -d)

# Install RabbitMQ using direct Kubernetes manifests
if ! kubectl get statefulset outpost-rabbitmq >/dev/null 2>&1; then
    echo "🐰 Installing RabbitMQ (using official rabbitmq:3.13-management-alpine image)..."
    
    # Generate passwords
    RABBITMQ_PASSWORD=$(openssl rand -hex 16)
    RABBITMQ_ERLANG_COOKIE=$(openssl rand -hex 32)
    
    # Create RabbitMQ secret
    kubectl create secret generic outpost-rabbitmq \
        --from-literal=rabbitmq-password="$RABBITMQ_PASSWORD" \
        --from-literal=rabbitmq-erlang-cookie="$RABBITMQ_ERLANG_COOKIE" \
        --dry-run=client -o yaml | kubectl apply -f - >/dev/null
    
    # Apply RabbitMQ manifests
    kubectl apply -f "$SCRIPT_DIR/rabbitmq.yaml" >/dev/null
    
    echo "⏳ Waiting for RabbitMQ to be ready (timeout: 120s)..."
    if ! wait_for_pod_ready "outpost-rabbitmq-0" 120; then
        echo ""
        echo "❌ RabbitMQ pod failed to become ready!"
        echo ""
        echo "Pod status:"
        kubectl get pod outpost-rabbitmq-0
        echo ""
        echo "Recent events:"
        kubectl describe pod outpost-rabbitmq-0 | grep -A 10 "Events:" || true
        echo ""
        echo "Container logs (last 20 lines):"
        kubectl logs outpost-rabbitmq-0 --tail=20 2>/dev/null || echo "  (no logs available)"
        echo ""
        echo "⚠️  To fix this issue, clean up and re-run:"
        echo "     kubectl delete -f $SCRIPT_DIR/rabbitmq.yaml"
        echo "     kubectl delete pvc data-outpost-rabbitmq-0"
        echo "     kubectl delete secret outpost-rabbitmq"
        echo ""
        exit 1
    fi
elif ! pod_is_healthy "outpost-rabbitmq-0"; then
    echo "❌ RabbitMQ pod is unhealthy!"
    echo ""
    echo "Pod status:"
    kubectl get pod outpost-rabbitmq-0
    echo ""
    echo "Recent events:"
    kubectl describe pod outpost-rabbitmq-0 | grep -A 10 "Events:" || true
    echo ""
    echo "Container logs (last 20 lines):"
    kubectl logs outpost-rabbitmq-0 --tail=20 2>/dev/null || echo "  (no logs available)"
    echo ""
    echo "⚠️  To fix this issue, clean up and re-run:"
    echo "     kubectl delete -f $SCRIPT_DIR/rabbitmq.yaml"
    echo "     kubectl delete pvc data-outpost-rabbitmq-0"
    echo "     kubectl delete secret outpost-rabbitmq"
    echo ""
    exit 1
else
    echo "🐰 RabbitMQ already installed and healthy, skipping..."
fi
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
   helm install outpost ../../deployments/kubernetes/charts/outpost"