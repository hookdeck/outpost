#!/bin/bash
set -e

# Function to log with timestamp
log() {
    local timestamp="[$(date '+%Y-%m-%d %H:%M:%S')]"
    echo "$timestamp $1"
}

# Function to handle errors
handle_error() {
    local exit_code=$?
    log "❌ Error occurred (exit code: $exit_code)"
    exit $exit_code
}

# Set error handler
trap handle_error ERR

log "🔄 Checking kubectl configuration..."

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

# Create monitoring namespace
MONITORING_NAMESPACE="monitoring"
log "🔄 Creating namespace: $MONITORING_NAMESPACE..."
kubectl create namespace $MONITORING_NAMESPACE --dry-run=client -o yaml | kubectl apply -f -

# Add Prometheus Helm repo if needed
log "📦 Adding Prometheus Helm repo..."
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Install Prometheus and Grafana
log "🚀 Installing Prometheus and Grafana..."
helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
    --namespace $MONITORING_NAMESPACE \
    --set grafana.service.type=LoadBalancer \
    --set prometheus.service.type=ClusterIP \
    --set alertmanager.service.type=ClusterIP \
    --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
    --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false \
    --wait

# Wait for Grafana to be ready
log "⏳ Waiting for Grafana deployment to be ready..."
kubectl rollout status deployment/monitoring-grafana -n $MONITORING_NAMESPACE --timeout=300s

# Get Grafana admin password
GRAFANA_PASSWORD=$(kubectl get secret -n $MONITORING_NAMESPACE monitoring-grafana -o jsonpath="{.data.admin-password}" | base64 --decode)

# Get Grafana URL
GRAFANA_URL=$(kubectl get svc -n $MONITORING_NAMESPACE monitoring-grafana -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')

# Check for the dashboard file
DASHBOARD_PATH="../../../loadtest/grafana/outpost-dashboard.json"
if [ ! -f "$DASHBOARD_PATH" ]; then
    log "⚠️ Outpost dashboard file not found at $DASHBOARD_PATH"
    # Try to find it using an absolute path
    SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
    REPO_ROOT="$( cd "$SCRIPT_DIR/../../../.." && pwd )"
    DASHBOARD_PATH="$REPO_ROOT/loadtest/grafana/outpost-dashboard.json"
    log "🔍 Trying alternate path: $DASHBOARD_PATH"
fi

# Import Outpost dashboard if it exists
if [ -f "$DASHBOARD_PATH" ]; then
    log "📊 Dashboard file found. Will import to Grafana once available..."
    
    # Wait for the load balancer to be ready
    log "⏳ Waiting for Grafana load balancer to be assigned an address (this can take a few minutes)..."
    TIMEOUT_COUNT=0
    while [ -z "$GRAFANA_URL" ] || [ "$GRAFANA_URL" == "pending" ]; do
        sleep 10
        GRAFANA_URL=$(kubectl get svc -n $MONITORING_NAMESPACE monitoring-grafana -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')
        TIMEOUT_COUNT=$((TIMEOUT_COUNT+1))
        
        if [ $TIMEOUT_COUNT -ge 30 ]; then
            log "⚠️ Timed out waiting for LoadBalancer address. You can import the dashboard manually later."
            break
        fi
    done
    
    if [ -n "$GRAFANA_URL" ] && [ "$GRAFANA_URL" != "pending" ]; then
        # Wait a bit more for Grafana to be fully ready and DNS to propagate
        log "⏳ Waiting for Grafana to be fully accessible..."
        sleep 30
        
        # Import dashboard
        log "📊 Importing Outpost dashboard to Grafana..."
        curl -X POST "http://admin:$GRAFANA_PASSWORD@$GRAFANA_URL/api/dashboards/db" \
            -H "Content-Type: application/json" \
            -d @$DASHBOARD_PATH

        IMPORT_STATUS=$?
        if [ $IMPORT_STATUS -ne 0 ]; then
            log "⚠️ Failed to import dashboard automatically. You can import it manually later."
        else
            log "✅ Dashboard imported successfully!"
        fi
    else
        log "⚠️ Could not get Grafana URL. You'll need to import the dashboard manually."
    fi
else
    log "⚠️ Outpost dashboard file not found. You'll need to import it manually."
fi

# Print success message
log "✅ Monitoring setup complete!

Grafana Details:
--------------
URL: http://$GRAFANA_URL
Username: admin
Password: $GRAFANA_PASSWORD
(Note: It may take a few minutes for the DNS to propagate)

Manual Dashboard Import (if needed):
-----------------------------------
1. Go to http://$GRAFANA_URL and log in with the credentials above
2. Navigate to Dashboards > Import
3. Upload the JSON file from: $DASHBOARD_PATH

Useful Commands:
--------------
# View Grafana logs:
kubectl logs -f -l app.kubernetes.io/name=grafana -n $MONITORING_NAMESPACE

# View Prometheus logs:
kubectl logs -f -l app.kubernetes.io/name=prometheus -n $MONITORING_NAMESPACE

# Port forward for Prometheus UI (if needed):
kubectl port-forward svc/monitoring-kube-prometheus-prometheus 9090:9090 -n $MONITORING_NAMESPACE

# Get Grafana and Prometheus services:
kubectl get svc -n $MONITORING_NAMESPACE

Service Discovery:
---------------
Prometheus will automatically discover and scrape metrics from pods with
the following annotations:
  prometheus.io/scrape: 'true'
  prometheus.io/path: '/metrics'
  prometheus.io/port: '8080'
"

log "👉 Monitoring is now set up! You can access Grafana at http://$GRAFANA_URL"
