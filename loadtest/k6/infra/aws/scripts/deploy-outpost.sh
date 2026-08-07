#!/bin/bash
set -e

# Function to log with timestamp
log() {
    local timestamp="[$(date '+%Y-%m-%d %H:%M:%S')]"
    echo "$timestamp $1"
}

# Debug function to print current directory
debug_pwd() {
    log "DEBUG: Current directory is $(pwd)"
}

# Function to handle errors
handle_error() {
    local exit_code=$?
    log "❌ Error occurred (exit code: $exit_code)"
    exit $exit_code
}

# Set error handler
trap handle_error ERR

log "🔄 Checking AWS and kubectl configuration..."
debug_pwd

# Ensure AWS and kubectl are configured correctly
if ! aws sts get-caller-identity --profile outpost-loadtest > /dev/null; then
    log "❌ AWS profile 'outpost-loadtest' not configured properly."
    exit 1
fi

# Check if we can access the EKS cluster and verify it's the correct one
if ! kubectl get nodes > /dev/null; then
    log "⚙️ Updating kubeconfig for EKS cluster..."
    aws eks update-kubeconfig --name outpost-loadtest --profile outpost-loadtest
fi

# Verify we're connected to the correct cluster
CLUSTER_NAME=$(kubectl config current-context | grep -o 'outpost-loadtest')
if [[ "$CLUSTER_NAME" != "outpost-loadtest" ]]; then
    log "❌ Not connected to the outpost-loadtest cluster. Current context: $(kubectl config current-context)"
    log "⚙️ Switching kubeconfig to EKS cluster..."
    aws eks update-kubeconfig --name outpost-loadtest --profile outpost-loadtest --set-context
    
    # Check again after updating
    CLUSTER_NAME=$(kubectl config current-context | grep -o 'outpost-loadtest')
    if [[ "$CLUSTER_NAME" != "outpost-loadtest" ]]; then
        log "❌ Failed to connect to the outpost-loadtest cluster. Please check your AWS credentials and try again."
        exit 1
    fi
fi

# Verify the node count matches our expectations
NODE_COUNT=$(kubectl get nodes --no-headers | wc -l | tr -d ' ')
log "📊 Found $NODE_COUNT nodes in the cluster"
if [[ "$NODE_COUNT" -lt 1 ]]; then
    log "⚠️ Warning: Expected at least 1 node in the cluster, but found $NODE_COUNT"
    log "⚠️ You might need to scale your node group using: aws eks update-nodegroup-config --cluster-name outpost-loadtest --nodegroup-name outpost-loadtest --scaling-config desiredSize=X"
    read -p "Continue anyway? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Create namespace if it doesn't exist
NAMESPACE="outpost-loadtest-1"
log "🔄 Creating namespace: $NAMESPACE..."
kubectl create namespace $NAMESPACE --dry-run=client -o yaml | kubectl apply -f -

# Generate application secrets
log "🔑 Generating application secrets..."
API_KEY=$(openssl rand -hex 16)
API_JWT_SECRET=$(openssl rand -hex 32)
AES_ENCRYPTION_SECRET=$(openssl rand -hex 32)

# Get endpoints from Terraform outputs
log "📊 Retrieving AWS resource endpoints from Terraform..."
debug_pwd
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
log "DEBUG: SCRIPT_DIR is $SCRIPT_DIR"
cd "$SCRIPT_DIR/../terraform"
debug_pwd

# Check if Terraform state exists
if [ ! -f "terraform.tfstate" ]; then
    log "⚠️ Terraform state file not found. Make sure you've applied your Terraform configuration."
    read -p "Continue anyway? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
    # Use placeholders if continuing
    DB_HOST="NOT_CONFIGURED"
    REDIS_HOST="NOT_CONFIGURED"
else
    # Get the database and Redis endpoints
    DB_HOST=$(terraform output -raw rds_endpoint 2>/dev/null || echo "NOT_CONFIGURED")
    REDIS_HOST=$(terraform output -raw elasticache_endpoint 2>/dev/null || echo "NOT_CONFIGURED")
fi

cd "$SCRIPT_DIR"
debug_pwd

# Validate endpoints
if [[ "$DB_HOST" == "NOT_CONFIGURED" ]]; then
    log "⚠️ Database endpoint not found in Terraform outputs."
    read -p "Continue anyway? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

if [[ "$REDIS_HOST" == "NOT_CONFIGURED" ]]; then
    log "⚠️ Redis endpoint not found in Terraform outputs."
    read -p "Continue anyway? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Create Kubernetes secrets
log "🔒 Creating Kubernetes secrets..."
kubectl create secret generic outpost-secrets \
    --namespace $NAMESPACE \
    --from-literal=POSTGRES_URL="postgresql://postgres:temppassword@$DB_HOST/outpost?sslmode=disable" \
    --from-literal=REDIS_HOST="$REDIS_HOST" \
    --from-literal=REDIS_PORT="6379" \
    --from-literal=AWS_ACCESS_KEY_ID="$(aws configure get aws_access_key_id --profile outpost-loadtest)" \
    --from-literal=AWS_SECRET_ACCESS_KEY="$(aws configure get aws_secret_access_key --profile outpost-loadtest)" \
    --from-literal=AWS_REGION="$(aws configure get region --profile outpost-loadtest)" \
    --from-literal=API_KEY="$API_KEY" \
    --from-literal=API_JWT_SECRET="$API_JWT_SECRET" \
    --from-literal=AES_ENCRYPTION_SECRET="$AES_ENCRYPTION_SECRET" \
    --save-config --dry-run=client -o yaml | kubectl apply -f -

# Check if any NGINX Ingress Controller is installed
NGINX_INGRESS_NAMESPACE=$(kubectl get ns -o name 2>/dev/null | grep ingress-nginx || echo "")
NGINX_INGRESS_DEPLOYED=$(kubectl get deployment --all-namespaces -o wide 2>/dev/null | grep -i ingress | wc -l | tr -d ' ')

if [[ -z "$NGINX_INGRESS_NAMESPACE" && "$NGINX_INGRESS_DEPLOYED" -eq 0 ]]; then
    log "⚠️ No Ingress Controller found. Installing NGINX Ingress Controller..."
    
    # Create namespace if it doesn't exist
    kubectl create namespace ingress-nginx --dry-run=client -o yaml | kubectl apply -f -
    
    # Add repo and install
    helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
    helm repo update
    
    # Install using a different release name to avoid conflicts
    helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
        --namespace ingress-nginx \
        --wait
else
    log "✅ Ingress Controller already installed, skipping installation"
fi
debug_pwd

# Find the Helm chart path and values file
log "DEBUG: About to calculate paths. SCRIPT_DIR is $SCRIPT_DIR"
REPO_ROOT="$( cd "$SCRIPT_DIR/../../../.." && pwd )"
log "DEBUG: REPO_ROOT is $REPO_ROOT"
CHART_PATH="$REPO_ROOT/deployments/kubernetes/charts/outpost"
VALUES_PATH="$REPO_ROOT/loadtest/infra/aws/values/outpost/values.yaml"
log "DEBUG: After path calculation. Current dir is $(pwd)"

# Check if chart and values file exist
if [ ! -d "$CHART_PATH" ]; then
    log "❌ Helm chart not found at $CHART_PATH. Please check the repository structure."
    exit 1
fi

if [ ! -f "$VALUES_PATH" ]; then
    log "❌ Values file not found at $VALUES_PATH. Please check the repository structure."
    exit 1
fi

# Install Outpost using Helm chart with values file
log "🚀 Installing Outpost..."
helm upgrade --install outpost $CHART_PATH \
    --namespace $NAMESPACE \
    --values $VALUES_PATH \
    --wait

# Verify Outpost is running
log "🔄 Verifying deployment..."
kubectl rollout status deployment/outpost-api -n $NAMESPACE --timeout=300s

# Get the ingress hostname
INGRESS_HOSTNAME=$(kubectl get ingress -n $NAMESPACE outpost -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || echo "pending")

# Wait for the ingress to get an address
if [[ "$INGRESS_HOSTNAME" == "pending" ]]; then
    log "⏳ Waiting for ingress to get an address (this may take a few minutes)..."
    
    TIMEOUT_COUNT=0
    while [[ "$INGRESS_HOSTNAME" == "pending" || -z "$INGRESS_HOSTNAME" ]]; do
        sleep 10
        INGRESS_HOSTNAME=$(kubectl get ingress -n $NAMESPACE outpost -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || echo "pending")
        TIMEOUT_COUNT=$((TIMEOUT_COUNT+1))
        
        if [ $TIMEOUT_COUNT -ge 30 ]; then
            log "⚠️ Timed out waiting for ingress address."
            break
        fi
    done
fi

# Print success message
log "✅ Outpost deployment complete!

Environment Details:
------------------
Namespace: $NAMESPACE
API Key: $API_KEY

Access Information:
----------------
Ingress Hostname: $INGRESS_HOSTNAME
(Note: It might take a few minutes for DNS to propagate)

Database Connection:
------------------
PostgreSQL Host: $DB_HOST

Redis Connection:
---------------
Redis Host: $REDIS_HOST

Useful Commands:
--------------
# View Outpost API logs:
kubectl logs -f -l app.kubernetes.io/name=outpost,app.kubernetes.io/component=api -n $NAMESPACE

# View Outpost delivery logs:
kubectl logs -f -l app.kubernetes.io/name=outpost,app.kubernetes.io/component=delivery -n $NAMESPACE

# View Outpost log service logs:
kubectl logs -f -l app.kubernetes.io/name=outpost,app.kubernetes.io/component=log -n $NAMESPACE

# Port forward if needed for local testing:
kubectl port-forward svc/outpost 3333:3333 -n $NAMESPACE

# Get ingress address:
kubectl get ingress -n $NAMESPACE

Example API Call:
---------------
# Health check (replace the hostname with your actual ingress hostname):
curl http://$INGRESS_HOSTNAME/api/v1/healthz

# Or with port-forwarding:
# kubectl port-forward svc/outpost 3333:3333 -n $NAMESPACE
# curl http://localhost:3333/api/v1/healthz
"

log "👉 Next, run $SCRIPT_DIR/deploy-monitoring.sh to set up Prometheus and Grafana for monitoring"
