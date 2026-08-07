# Infrastructure Setup for Loadtesting

This document provides information on setting up different infrastructure environments for Outpost loadtesting. Currently, only local environment setup is supported.

## Local Environment

The local infrastructure setup includes scripts and Kubernetes configuration files to help you deploy the Outpost service with RabbitMQ locally.

### Setup Options

There are two deployment size options available:
- **small** (default)
- **medium**

### Getting Started

Navigate to the infrastructure directory first:

```
cd infra/local/kubernetes
```

### Deployment

To deploy the local environment, run:

```
./scripts/up.sh
```

Or specify a different size:

```
./scripts/up.sh --size=medium
```

This will provision the full Outpost services in a new namespace.

### Ingress Setup

The Outpost services are exposed via ingress, so you'll need to have minikube ingress enabled and run minikube tunnel:

```
minikube addons enable ingress
minikube tunnel
```

Default ingress URLs:
- For small environment: `outpost.acme.local`
- For medium environment: `outpost-medium.acme.local`

Add these entries to your `/etc/hosts` file:

```
127.0.0.1 outpost.acme.local
127.0.0.1 outpost-medium.acme.local
```

When the environment is running, confirm that it's working as expected by running a health check:

```
curl outpost.acme.local/api/v1/healthz
```

### Monitoring

Grafana and Prometheus are used to observe resource consumption of the local services.

Install the Kubernetes Grafana and Prometheus setup using Helm:

```
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install monitoring prometheus-community/kube-prometheus-stack
```

Expose Grafana:

```
kubectl port-forward -n monitoring svc/prometheus-grafana 3000:80
```


Set up the custom dashboard with this curl command:

```
curl -X POST http://localhost:3000/api/dashboards/db -H "Content-Type: application/json" -d @loadtest/k6/grafana/outpost-dashboard.json
```

Navigate to `http://localhost:3000` in your browser to access Grafana.
- Username: admin
- Password: prom-operator

### Cleanup

To clean up and remove the deployed resources:

```
./scripts/down.sh
```