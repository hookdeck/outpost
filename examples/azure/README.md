# Deploy Outpost on Azure with Azure Service Bus

This example demonstrates how to deploy Outpost on Azure, using Azure Service Bus as the message queue.

## Azure Managed Redis Configuration

Azure Managed Redis requires cluster mode to be explicitly enabled:

```bash
# Required for Azure Managed Redis Enterprise
REDIS_CLUSTER_ENABLED=true
REDIS_TLS_ENABLED=true
REDIS_HOST="your-redis.westeurope.redisenterprise.cache.azure.net"
REDIS_PORT=10000
REDIS_PASSWORD="your-redis-password"
REDIS_DATABASE=0  # Ignored in cluster mode
```

## Azure Cache for Redis Configuration  

Azure Cache for Redis uses single-node mode:

```bash
# For Azure Cache for Redis (older service)
REDIS_CLUSTER_ENABLED=false
REDIS_TLS_ENABLED=true
REDIS_HOST="your-cache.redis.cache.windows.net"
REDIS_PORT=6380
REDIS_PASSWORD="your-cache-password"
REDIS_DATABASE=0
```

## Local Development Configuration

For local Redis development:

```bash
# For local/self-hosted Redis
REDIS_CLUSTER_ENABLED=false
REDIS_TLS_ENABLED=false
REDIS_HOST="localhost"
REDIS_PORT=6379
REDIS_PASSWORD=""
REDIS_DATABASE=0
```

**Important**: The `REDIS_CLUSTER_ENABLED` setting is **required** for Azure Managed Redis to prevent Redis clustering errors. Without this setting, you may see `EXECABORT Transaction discarded` errors.

### Azure Managed Redis Benefits

Azure Managed Redis (Redis Enterprise) offers significant advantages over Azure Cache for Redis:

- **Performance**: Up to 15x better performance than Azure Cache for Redis
- **Modern Features**: Redis 7.4+ with JSON, vector, time series, and probabilistic data types  
- **Enhanced Security**: TLS encryption by default, Microsoft EntraID integration
- **Better Pricing**: Generally more cost-effective than Azure Cache for Redis
- **High Availability**: 99.999% SLA potential with built-in clustering

Before you begin, ensure you have the following:

*   [Azure CLI](https://docs.microsoft.com/en-us/cli/azure/install-azure-cli) installed.
*   You are logged into your Azure account (`az login`).

## Architecture

This example deploys a distributed architecture on Azure, leveraging managed services for dependencies and Azure Container Apps for the Outpost services.

```mermaid
graph TD
    subgraph "External Source"
        WebhookSource[Event Source]
    end

    subgraph "Azure Container Apps"
        APIService["api service (Container)"]
        DeliveryService["delivery service (Container)"]
        LogService["log service (Container)"]
    end

    subgraph "Azure Dependencies"
        PostgresDB["PostgreSQL (Log Storage)"]
        ServiceBusDelivery["Azure Service Bus (Delivery Queue)"]
        ServiceBusLog["Azure Service Bus (Log Queue)"]
        RedisCache["Redis (Entity Storage)"]
    end

    subgraph "External Destinations"
        Delivery[Destination]
    end

    WebhookSource -- "HTTPS Request" --> APIService

    APIService -- " " --> PostgresDB
    APIService -- "Stores/Retrieves Session Data" --> RedisCache
    APIService -- "Enqueues Message" --> ServiceBusDelivery

    ServiceBusDelivery -- "Consumes Message" --> DeliveryService
    DeliveryService -- " " --> RedisCache
    DeliveryService -- "Enqueues Log" --> ServiceBusLog

    ServiceBusLog -- "Consumes Log" --> LogService
    LogService -- "Writes Log" --> PostgresDB

    DeliveryService -- "Sends Events" --> Delivery
```

### Components

#### Dependencies
The deployment relies on Azure-managed services for its core dependencies:
*   **PostgreSQL**: Used for persistent log storage (`log storage`).
*   **Redis**: Used for entity storage and caching (`entity storage`).
*   **Azure Service Bus**: Used as the message queue for both the delivery (`delivery queue`) and log (`log queue`) services.

#### Outpost Services
The Outpost application itself is deployed as three distinct services in Azure Container Apps:
*   **api**: The public-facing API that receives webhooks (`API Service`).
*   **delivery**: A backend service that processes and delivers webhooks from the queue (`Delivery Service`).
*   **log**: A backend service that processes and stores logs (`log service`).

## Scripts

This example includes three main scripts to manage the deployment:

*   `dependencies.sh`: Provisions all the necessary Azure resources, including PostgreSQL for storage, Redis for caching, and Azure Service Bus for the message queue. It also configures the required permissions for the services to interact with each other. If a PostgreSQL password is not provided via the `PG_PASS` environment variable, a secure one will be generated automatically.
*   `local-deploy.sh`: Deploys the Outpost services using Docker Compose. It uses the Azure resources provisioned by the `dependencies.sh` script.
*   `diagnostics.sh`: Runs checks to validate deployments. Use `--local` for the Docker deployment or `--azure` for the Azure Container Apps deployment. The script requires a webhook URL for testing, which can be provided via the `--webhook-url` flag or the `WEBHOOK_URL` environment variable.
*   `azure-deploy.sh`: Deploys the Outpost services to Azure Container Apps.

## Deployment Steps using Outpost Locally

To deploy Outpost, you must run the scripts in the following order:

1.  **Provision Dependencies:**
    ```bash
    # To use a specific password (optional):
    # export PG_PASS=<YOUR_POSTGRES_PASSWORD>
    ./dependencies.sh
    ```

2.  **Deploy Outpost:**
    ```bash
    ./local-deploy.sh
    ```

3.  **Run Diagnostics:**
    This command specifically targets the local Docker deployment. You will need a public webhook URL for the test.
    ```bash
    export WEBHOOK_URL=<YOUR_PUBLIC_WEBHOOK_URL>
    bash ./diagnostics.sh --local
    ```
    Alternatively, you can use the `--webhook-url` flag:
    ```bash
    bash ./diagnostics.sh --local --webhook-url <YOUR_PUBLIC_WEBHOOK_URL>
    ```
    
## Deploying Outpost to Azure Container Apps

### 1. Deploy with the Deployment Script (Recommended)

1.  **Prepare Environment Files:**
    Before deploying, you need the `.env.outpost` and `.env.runtime` files.

    *   **To provision new dependencies (Recommended):** Run the scripts to generate the files automatically.
        ```bash
        # To use a specific password (optional):
        # export PG_PASS=<YOUR_POSTGRES_PASSWORD>
        ./dependencies.sh
        ./local-deploy.sh
        ```
        > **Note:** The `local-deploy.sh` script will also start services locally via Docker Compose. You can stop them with `docker-compose down` after the script finishes if you only intend to deploy to ACA.

    *   **To use existing dependencies:** Create `.env.outpost` and `.env.runtime` manually. Refer to the [Environment Variable Reference](#environment-variable-reference) section for details on the required variables.

2.  **Run the Deployment Script:**
    Once the environment files are generated, run the `azure-deploy.sh` script to deploy the Outpost services to Azure Container Apps.

    ```bash
    ./azure-deploy.sh
    ```

### 2. Deploy Manually

These instructions outline how to manually deploy Outpost to Azure Container Apps (ACA).

#### 1. Prepare Environment Files

Before deploying manually, ensure you have the `.env.outpost` and `.env.runtime` files.

*   **To provision new dependencies:** Run the scripts to generate the files automatically.
    ```bash
    # To use a specific password (optional):
    # export PG_PASS=<YOUR_POSTGRES_PASSWORD>
    ./dependencies.sh
    ./local-deploy.sh
    ```
    > **Note:** The `local-deploy.sh` script will also start services locally via Docker Compose. You can stop them with `docker-compose down` after the script finishes if you only intend to deploy to ACA.

*   **To use existing dependencies:** Create `.env.outpost` and `.env.runtime` manually. Refer to the [Environment Variable Reference](#environment-variable-reference) section for details on the required variables.

#### 2. Load Environment Variables

Load the environment variables from both `.env.outpost` and `.env.runtime` into your shell session. Sourcing `.env.runtime` last ensures that it can override any common variables.
```bash
source .env.outpost && source .env.runtime
```

#### 3. Create Azure Container Apps Environment

Create the ACA Environment using the variables loaded in the previous step.

```bash
az containerapp env create \
  --name outpost-environment \
  --resource-group $RESOURCE_GROUP \
  --location $LOCATION
```

#### 4. Deploy Each Container

Deploy each service as a separate container app. First, the environment variables from `.env.runtime` must be formatted into a string that can be passed to the Azure CLI.

```bash
ENV_VARS_STRING=$(grep -v '^#' .env.runtime | sed 's/^export //g' | sed 's/"//g' | tr '\n' ' ')
```

Now, deploy the containers using the created string.

**Deploy `api` service:**
```bash
az containerapp create \
  --name outpost-api \
  --resource-group $RESOURCE_GROUP \
  --environment outpost-environment \
  --image hookdeck/outpost:v0.4.0 \
  --target-port 3333 \
  --ingress external \
  --env-vars "SERVICE=api" $ENV_VARS_STRING
```

**Deploy `delivery` service:**
```bash
az containerapp create \
  --name outpost-delivery \
  --resource-group $RESOURCE_GROUP \
  --environment outpost-environment \
  --image hookdeck/outpost:v0.4.0 \
  --ingress internal \
  --env-vars "SERVICE=delivery" $ENV_VARS_STRING
```

**Deploy `log` service:**
```bash
az containerapp create \
  --name outpost-log \
  --resource-group $RESOURCE_GROUP \
  --environment outpost-environment \
  --image hookdeck/outpost:v0.4.0 \
  --ingress internal \
  --env-vars "SERVICE=log" $ENV_VARS_STRING
```

### 3. Validate the Deployment

After deploying with either method, you can validate the deployment by running the following script. You will need a public webhook URL for the test.

```bash
export WEBHOOK_URL=<YOUR_PUBLIC_WEBHOOK_URL>
bash ./diagnostics.sh --azure
```
Alternatively, you can use the `--webhook-url` flag:
```bash
bash ./diagnostics.sh --azure --webhook-url <YOUR_PUBLIC_WEBHOOK_URL>
```

## Azure Deployment Options: Overview, Pros & Cons

There are several ways to deploy Docker containers to Azure. Below is a summary of the main options, their advantages, and trade-offs:

### 1. Azure Container Apps (ACA)

**Description:** Managed service for running containerized applications with built-in scaling, ingress, and environment management. Each service is deployed as a separate container app.  
**Pros:**
- No infrastructure management (serverless experience)
- Built-in scaling and ingress
- Easy integration with Azure resources
- Good fit for microservices and distributed architectures  
**Cons:**
- Limited support for multi-container orchestration compared to Docker Compose
- Some advanced networking features may require extra configuration
- Not suitable for very complex container topologies

### 2. Azure Container Instances (ACI)

**Description:** Directly run containers in Azure without managing VMs. Supports single containers or simple container groups.  
**Pros:**
- Fast and simple deployment
- No infrastructure management
- Good for short-lived or simple workloads  
**Cons:**
- Limited orchestration (multi-container support is basic)
- No built-in scaling or ingress
- Not ideal for production microservices

### 3. Azure Kubernetes Service (AKS)

**Description:** Full Kubernetes orchestration for complex, scalable deployments. Can convert Docker Compose files to Kubernetes manifests using tools like `kompose`.  
**Pros:**
- Full orchestration and scaling
- Advanced networking and service discovery
- Suitable for large-scale, complex applications  
**Cons:**
- Requires Kubernetes knowledge and management
- More operational overhead
- Overkill for simple deployments

### 4. Azure Web App for Containers

**Description:** Deploy container images to Azure App Service. Best for web apps and APIs.  
**Pros:**
- Simple deployment for web-facing apps
- Built-in scaling and monitoring  
**Cons:**
- Limited control over networking and container lifecycle
- Not suitable for multi-service architectures

## Using Docker Compose for Azure Deployments

Azure does not natively support `docker-compose.yml` for multi-container deployments in the same way as local Docker Compose. You can use conversion tools (e.g., `kompose` for AKS, or limited support for ACI) to generate Azure-compatible deployment files, but manual adjustments are often required.

## Why `azure-deploy.sh` Is a Reasonable Approach

The provided `azure-deploy.sh` script is a practical solution for deploying Outpost to Azure Container Apps because:

- It automates the deployment of multiple services, mirroring the local Docker Compose setup.
- It leverages environment files generated by your local setup, ensuring consistency between local and cloud deployments.
- It uses Azure CLI commands, which are well-supported and documented.
- It validates the deployment and provides immediate feedback (logs, health checks).
- It aligns with ACA’s architecture by deploying each service as a separate container app, following best practices for microservices.
- It handles Azure resource provider registration and environment creation, reducing manual setup steps and potential errors.
- It supports repeatable, scriptable deployments, making it easy to update, redeploy, or share the deployment process with others.
- It enables troubleshooting and monitoring by fetching logs and checking health endpoints immediately after deployment.
- It is flexible and can be adapted for different environments or configurations by simply updating environment files or script parameters.

**Summary:**  
For most users, `azure-deploy.sh` offers a balance of automation, reliability, maintainability, and alignment with Azure Container Apps best practices. It is recommended for Outpost deployments unless you require advanced orchestration (AKS) or have very simple workloads (ACI).

**Summary:**  
For most users, `azure-deploy.sh` offers a balance of automation, reliability, and maintainability. It is recommended for Outpost deployments unless you require advanced orchestration (AKS) or have very simple workloads (ACI).
## Environment Variable Reference

If you are not using the `dependencies.sh` and `local-deploy.sh` scripts to provision your infrastructure, you will need to create the `.env.outpost` and `.env.runtime` files manually.

See the [Configure Azure Service Bus as the Outpost Internal Message Queue](https://outpost.hookdeck.com/docs/guides/service-bus-internal-mq) guide for more details on the environment variables required for Outpost and how to create the values.

### `.env.outpost`

This file contains variables related to the Azure infrastructure where the services will be deployed.

| Variable | Description | Example |
| --- | --- | --- |
| `LOCATION` | The Azure region for your resources. | `westeurope` |
| `RESOURCE_GROUP` | The name of the Azure resource group. | `outpost-azure` |
| `POSTGRES_URL` | The full connection URL for your PostgreSQL database. | `postgres://user:pass@host:5432/dbname` |
| `REDIS_HOST` | The hostname of your Azure Managed Redis instance. | `outpost-redis.redisenterprise.cache.azure.net` |
| `REDIS_PORT` | The port for your Redis instance. | `10000` |
| `REDIS_PASSWORD` | The password for your Redis instance. | `your-redis-password` |
| `REDIS_DATABASE` | The Redis database number. | `0` |
| `REDIS_TLS_ENABLED` | Whether TLS is enabled for Redis connections. | `true` |
| `AZURE_SERVICEBUS_CLIENT_ID` | The Client ID of the service principal for Service Bus access. | `...` |
| `AZURE_SERVICEBUS_CLIENT_SECRET` | The Client Secret of the service principal. | `...` |
| `AZURE_SERVICEBUS_SUBSCRIPTION_ID` | Your Azure Subscription ID. | `...` |
| `AZURE_SERVICEBUS_TENANT_ID` | Your Azure Tenant ID. | `...` |
| `AZURE_SERVICEBUS_NAMESPACE` | The name of the Service Bus namespace. | `outpost-internal` |
| `AZURE_SERVICEBUS_RESOURCE_GROUP` | The resource group for the Service Bus. | `outpost-azure` |
| `AZURE_SERVICEBUS_DELIVERY_TOPIC` | The name of the delivery topic. | `outpost-delivery` |
| `AZURE_SERVICEBUS_DELIVERY_SUBSCRIPTION` | The name of the delivery subscription. | `outpost-delivery-sub` |
| `AZURE_SERVICEBUS_LOG_TOPIC` | The name of the log topic. | `outpost-log` |
| `AZURE_SERVICEBUS_LOG_SUBSCRIPTION` | The name of the log subscription. | `outpost-log-sub` |

### `.env.runtime`

This file contains secrets and runtime configuration for the Outpost services. It includes all variables from `.env.outpost` plus the following application-specific secrets.

| Variable | Description |
| --- | --- |
| `API_KEY` | A secret key for securing the Outpost API. |
| `API_JWT_SECRET` | A secret for signing JWTs. |
| `AES_ENCRYPTION_SECRET` | A secret for data encryption. |