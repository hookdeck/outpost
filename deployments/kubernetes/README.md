# Development Kubernetes Setup

Local Kubernetes setup for Outpost using Minikube. This setup includes:
- Outpost services (API, delivery, and log processors)
- Redis as KV & entity storage
- PostgreSQL as log storage
- RabbitMQ for message queuing

## Prerequisites

- [Docker](https://docs.docker.com/engine/install/)
- [Minikube](https://minikube.sigs.k8s.io/docs/start)
- [Kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/docs/intro/install/)

## Setup

1. Start Minikube and create namespace:
```sh
minikube start
kubectl create namespace outpost
kubectl config set-context --current --namespace=outpost
```

2. Install dependencies:
```sh
cd deployments/kubernetes
./setup-dependencies.sh
```

3. Install Outpost:
```sh
helm install outpost charts/outpost -f values.yaml
```

## Verify Installation

1. Setup ingress and tunnel (keep this terminal open):
```sh
minikube addons enable ingress
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=90s
sudo minikube tunnel
```

2. Add local domain to `/etc/hosts`:
```sh
echo "127.0.0.1 outpost.acme.local" | sudo tee -a /etc/hosts
```

3. Get your API key:
```sh
kubectl get secret outpost-secrets -o jsonpath='{.data.API_KEY}' | base64 -d
```

4. Test the API:
```sh
# Create tenant
curl -X PUT http://outpost.acme.local/api/v1/123 \
  -H "Authorization: YOUR_API_KEY"

# Query tenant
curl http://outpost.acme.local/api/v1/123 \
  -H "Authorization: YOUR_API_KEY"
```

## Explore Further

- Publish events to test delivery/log services
- Access infrastructure:
  ```sh
  # Redis CLI
  kubectl exec -it outpost-redis-master-0 -- redis-cli
  
  # PostgreSQL
  kubectl exec -it outpost-postgresql-0 -- psql -U outpost
  
  # RabbitMQ Management: http://localhost:15672
  kubectl port-forward outpost-rabbitmq-0 15672:15672
  ```

## Cleanup

```sh
# Remove Kubernetes resources
kubectl delete namespace outpost
kubectl config set-context --current --namespace=default

# Remove local domain
sudo sed -i '' '/outpost.acme.local/d' /etc/hosts
```

## TODO

- [ ] Complete Chart.yaml
- [ ] Add NOTES.txt
- [ ] Add values.schema.json
- [ ] Evaluate further configuration options