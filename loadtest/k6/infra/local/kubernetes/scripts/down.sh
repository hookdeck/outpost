#!/bin/bash
set -e

if [ -z "$1" ]; then
    echo "❌ Error: Namespace is required"
    echo "Usage: ./down.sh <namespace>"
    echo "Example: ./down.sh outpost-loadtest-1234567890"
    exit 1
fi

NAMESPACE=$1

echo "🧹 Cleaning up Outpost load test environment in namespace: $NAMESPACE..."

# Check if namespace exists
if ! kubectl get namespace $NAMESPACE >/dev/null 2>&1; then
    echo "❌ Error: Namespace $NAMESPACE not found"
    exit 1
fi

# Delete the namespace (this will delete everything in it)
echo "🗑️  Removing namespace and all resources..."
kubectl delete namespace $NAMESPACE

echo "✅ Cleanup complete!" 