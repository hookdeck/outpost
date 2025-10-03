# Azure Service Bus Configuration Instructions

The Service Bus destination can be a topic or a queue.

## Configuration

- **Queue or Topic Name**: The name of the Service Bus topic or queue.
- **Connection String**: The connection string for the Service Bus instance.

---

## How to set up Azure Service Bus as an event destination using the Azure CLI

Here are the resources you'll need to set up:

1. Resource Group - A container that holds related Azure resources
2. Service Bus Namespace - The messaging service container (must be globally unique)
3. Topic or a Queue:
    - Topic: A message distribution mechanism (pub/sub pattern)
    - Queue: A message queue for point-to-point communication

### Prerequisites

Assuming you have an Azure account and an authenticated Azure CLI, here are the steps to set up a Service Bus Topic or Queue as an Event Destination.

Install [Azure CLI](https://docs.microsoft.com/en-us/cli/azure/install-azure-cli).

Log in to Azure:

```bash
az login
```

### 1. Create Resource Group

Set variables:

```bash
RESOURCE_GROUP="outpost-rg"
LOCATION="eastus"
```

Create resource group:

```bash
az group create --name $RESOURCE_GROUP --location $LOCATION
```

#### 2. Create Service Bus Namespace

Generate a unique namespace name (must be globally unique).

```bash
RANDOM_SUFFIX=$(openssl rand -hex 4)
NAMESPACE_NAME="outpost-servicebus-$RANDOM_SUFFIX"
```

Create Service Bus namespace:

```bash
az servicebus namespace create \
  --resource-group $RESOURCE_GROUP \
  --name $NAMESPACE_NAME \
  --location $LOCATION \
  --sku Standard
```

### 3. Create Topic/Queue-Specific Access Policy

Choose either a Topic or a Queue based on your requirements. Below are examples for both.

#### For a Topic

Set a topic name:

```bash
TOPIC_NAME="destination-test"
```

Create a topic:

```bash
az servicebus topic create \
  --resource-group $RESOURCE_GROUP \
  --namespace-name $NAMESPACE_NAME \
  --name $TOPIC_NAME
```

Create a send-only policy for the topic.

```bash
az servicebus topic authorization-rule create \
  --resource-group $RESOURCE_GROUP \
  --namespace-name $NAMESPACE_NAME \
  --topic-name $TOPIC_NAME \
  --name SendOnlyPolicy \
  --rights Send
```

Get the Topic-Specific Connection String:

```bash
az servicebus topic authorization-rule keys list \
  --resource-group $RESOURCE_GROUP \
  --namespace-name $NAMESPACE_NAME \
  --topic-name $TOPIC_NAME \
  --name SendOnlyPolicy \
  --query primaryConnectionString \
  --output tsv
```

This returns a connection string that can only send to the Service Bus topic. Use this connection string in the **Connection String** field of the Event Destination configuration.

```
Endpoint=sb://outpost-sb-a3f2b1.servicebus.windows.net/;SharedAccessKeyName=SendOnlyPolicy;SharedAccessKey=xyz789...;EntityPath=events
```

#### For a Queue

Set a queue name:

```bash
QUEUE_NAME="myqueue"
```

Create a queue:

```bash
az servicebus queue create \
  --resource-group $RESOURCE_GROUP \
  --namespace-name $NAMESPACE_NAME \
  --name $QUEUE_NAME
```

Create a send-only policy for the queue.

```bash
az servicebus queue authorization-rule create \
  --resource-group $RESOURCE_GROUP \
  --namespace-name $NAMESPACE_NAME \
  --queue-name $QUEUE_NAME \
  --name SendOnlyPolicy \
  --rights Send
```

Get the Queue-Specific Connection String:

```bash
az servicebus queue authorization-rule keys list \
  --resource-group $RESOURCE_GROUP \
  --namespace-name $NAMESPACE_NAME \
  --queue-name $QUEUE_NAME \
  --name SendOnlyPolicy \
  --query primaryConnectionString \
  --output tsv
```

This returns a connection string that can only send to the Service Bus queue. Use this connection string in the **Connection String** field of the Event Destination configuration.

```
Endpoint=sb://outpost-sb-a3f2b1.servicebus.windows.net/;SharedAccessKeyName=SendOnlyPolicy;SharedAccessKey=xyz789...;EntityPath=myqueue
```
