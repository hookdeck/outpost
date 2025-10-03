# Test AzureServiceBus Destination

Assuming you have an Azure account & an authenticated Azure CLI, here are the steps you can do to set up a test ServiceBus Topic & Subscription for testing.

Here are the resources you'll set up:

1. Resource Group - A container that holds related Azure resources
  - Name: outpost-demo-rg
2. Service Bus Namespace - The messaging service container (must be globally unique)
  - Name: outpost-demo-sb-[RANDOM] (e.g., outpost-demo-sb-a3f2b1)
3. Topic - A message distribution mechanism (pub/sub pattern)
  - Name: destination-test
4. Subscription - A receiver for messages sent to the topic
  - Name: destination-test-sub

Step 1: Create a Resource Group

az group create \
  --name outpost-demo-rg \
  --location eastus

Step 2: Create a Service Bus Namespace

# Generate a random suffix for uniqueness
RANDOM_SUFFIX=$(openssl rand -hex 3)

# Create the namespace
az servicebus namespace create \
  --resource-group outpost-demo-rg \
  --name outpost-demo-sb-${RANDOM_SUFFIX} \
  --location eastus \
  --sku Standard

Note: The namespace name must be globally unique. The Standard SKU is required for topics (Basic only
supports queues).

Step 3: Create a Topic

az servicebus topic create \
  --resource-group outpost-demo-rg \
  --namespace-name outpost-demo-sb-${RANDOM_SUFFIX} \
  --name destination-test

Step 4: Create a Subscription

az servicebus topic subscription create \
  --resource-group outpost-demo-rg \
  --namespace-name outpost-demo-sb-${RANDOM_SUFFIX} \
  --topic-name destination-test \
  --name destination-test-sub

Step 5: Get the Connection String

az servicebus namespace authorization-rule keys list \
  --resource-group outpost-demo-rg \
  --namespace-name outpost-demo-sb-${RANDOM_SUFFIX} \
  --name RootManageSharedAccessKey \
  --query primaryConnectionString \
  --output tsv

Example Output:
Endpoint=sb://outpost-demo-sb-a3f2b1.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;Sh
aredAccessKey=abcd1234...

You can then create an Azure destination with:
- name: "destination-test"
- connection string: "Endpoint=sb://outpost-demo-sb-a3f2b1.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;Sh
aredAccessKey=abcd1234..."
