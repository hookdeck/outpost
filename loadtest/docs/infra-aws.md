# AWS Infrastructure for Outpost Load Testing

## Overview

This guide helps set up a complete AWS infrastructure for load testing Outpost. We'll deploy a managed Kubernetes cluster with database and caching services.

Components:
- Amazon EKS: Managed Kubernetes cluster to run Outpost services
- Amazon RDS: PostgreSQL database for persistent storage
- Amazon ElastiCache: Redis instance for caching and queues
- Kubernetes add-ons: Dashboard, Prometheus, Grafana for monitoring

## Steps

### 1. Configure AWS CLI

Set up your AWS credentials with the `outpost-loadtest` profile:

```sh
aws configure --profile outpost-loadtest
```

All Terraform configurations and scripts in this repository assume the use of this specific profile. Make sure your credentials have sufficient permissions to create EKS clusters, RDS instances, and other AWS resources.

### 2. Run Terraform

Navigate to the terraform directory and initialize the Terraform modules:

```sh
cd loadtest/infra/aws/terraform
terraform init
```

Then apply the configuration to create all AWS resources:

```sh
terraform apply
```

This will provision:
- EKS cluster with worker nodes
- RDS PostgreSQL instance
- ElastiCache Redis cluster
- Kubernetes Dashboard
- Required networking components (VPC, subnets, security groups)

The process takes approximately 15-20 minutes to complete. You'll be prompted to confirm before resources are created.

### 3. Deploy Monitoring

After Terraform has successfully provisioned the infrastructure, deploy the monitoring stack:

```sh
cd loadtest/infra/aws
chmod +x scripts/deploy-monitoring.sh
./scripts/deploy-monitoring.sh
```

This script installs Prometheus and Grafana on the EKS cluster for comprehensive monitoring of Outpost performance and resource usage.

### 4. Deploy Outpost

Deploy the Outpost application using the Helm chart:

```sh
cd loadtest/infra/aws
chmod +x scripts/deploy-outpost.sh
./scripts/deploy-outpost.sh
```

This script uses Helm to deploy Outpost with the configuration defined in `infra/aws/values/outpost/values.yaml`. The values file contains settings for image repository, resource limits, environment variables, and service configuration.

You can customize the deployment by modifying the values file before running the script.

### 5. Confirm Setup

#### 5.1 Access Kubernetes Dashboard

Access the Kubernetes Dashboard by port-forwarding to your local machine:

```sh
kubectl port-forward -n kubernetes-dashboard service/kubernetes-dashboard 8443:443
```

Then open in your browser:
- URL: https://localhost:8443

Generate a token for dashboard authentication:

```sh
kubectl -n kubernetes-dashboard create token admin-user
```

#### 5.2 Access Grafana

Get the Grafana URL:

```sh
kubectl get svc -n monitoring monitoring-grafana -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
```

Grafana credentials:
- Username: admin
- Password: prom-operator

#### 5.3 Verify Outpost

Get the Outpost API URL:

```sh
export API_URL=$(kubectl get svc -n outpost-loadtest-1 outpost -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')
echo $API_URL
```

Get the API key:

```sh
export API_KEY=$(kubectl get secret -n outpost-loadtest-1 outpost-secrets -o jsonpath='{.data.API_KEY}' | base64 --decode)
echo $API_KEY
```

Check the health endpoint:

```sh
curl $API_URL/api/v1/healthz
```

A successful health check will return a positive response indicating that Outpost is up and running correctly.

#### 5.4 Create New Tenant

Once Outpost is running, you can create a new tenant with:

```sh
curl -v --location --request PUT "$API_URL/api/v1/123" \
--header "Authorization: Bearer $API_KEY"
```

This creates a tenant with ID `123` which can be used for testing.

## Other Configuration

### Outpost Release / Customize Environment

You can customize the Outpost deployment by modifying the values in `loadtest/infra/aws/values/outpost/values.yaml`:

- Change resource limits and requests
- Adjust replica counts for services
- Modify environment variables
- Configure LoadBalancer settings
- Customize Outpost configuration (publish/delivery concurrency, retry limits, etc.)

After making changes to the values file, redeploy Outpost using the following command:

```sh
cd loadtest/infra/aws
helm upgrade --install outpost ../../../deployments/kubernetes/charts/outpost \
  --namespace outpost-loadtest-1 \
  -f ./values/outpost/values.yaml
```

For configuration or environment variable changes without image updates, you'll need to manually restart the deployments:

```sh
kubectl rollout restart deployment -n outpost-loadtest-1
```

Note: This manual restart will be handled in future versions of the Helm chart, but is required for now.

### Custom Image / ECR

For testing with custom builds, you can use your own Docker image instead of the public Hookdeck image:

#### 1. Build the Docker image

Build your Outpost image (example using goreleaser):

```sh
goreleaser release -f ./build/.goreleaser.yaml --snapshot --clean
```

This should create an image tagged as `hookdeck/outpost:latest-amd64`.

#### 2. Create an ECR repository

```sh
aws ecr create-repository --repository-name outpost --profile outpost-loadtest
```

#### 3. Tag and push the image to ECR

First, authenticate to ECR:

```sh
aws ecr get-login-password --profile outpost-loadtest | docker login --username AWS --password-stdin $(aws sts get-caller-identity --profile outpost-loadtest --query Account --output text).dkr.ecr.$(aws configure get region --profile outpost-loadtest).amazonaws.com
```

Then, tag and push the image:

```sh
ACCOUNT_ID=$(aws sts get-caller-identity --profile outpost-loadtest --query Account --output text)
REGION=$(aws configure get region --profile outpost-loadtest)
ECR_URI=${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com/outpost

docker tag hookdeck/outpost:latest-amd64 ${ECR_URI}:latest
docker push ${ECR_URI}:latest
```

#### 4. Update the Helm values

Get the full ECR URI for your values file:

```sh
echo "ECR URI: ${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com/outpost"
```

Update the image repository and tag in `loadtest/infra/aws/values/outpost/values.yaml`:

```yaml
outpost:
  image:
    repository: 442042537672.dkr.ecr.us-east-1.amazonaws.com/outpost  # Replace with your ECR URI
    tag: latest
```

Then deploy a new Outpost release following the instructions in [Outpost Release / Customize Environment](#outpost-release--customize-environment).

## Cleanup

To remove all resources:

```sh
cd loadtest/infra/aws
chmod +x scripts/cleanup.sh
./scripts/cleanup.sh
```

This will:
1. Remove all Kubernetes resources (Outpost and monitoring)
2. Optionally destroy AWS infrastructure with Terraform

## Troubleshooting

If you encounter issues during deployment or operation, try these troubleshooting steps:

- Check EKS cluster status: `aws eks describe-cluster --name outpost-loadtest --profile outpost-loadtest`
- Check Kubernetes pods: `kubectl get pods -A`
- Check Helm releases: `helm list -A`
- View pod logs: `kubectl logs -n outpost-loadtest-1 <pod-name>`
- Describe failing pods: `kubectl describe pod -n outpost-loadtest-1 <pod-name>`
