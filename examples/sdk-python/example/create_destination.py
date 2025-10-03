import os
import sys
import questionary
from dotenv import load_dotenv
from outpost_sdk import Outpost, models


def run():
    """
    Interactively creates a new destination for a tenant.
    """
    load_dotenv()

    admin_api_key = os.getenv("ADMIN_API_KEY")
    tenant_id = os.getenv("TENANT_ID", "hookdeck")
    server_url = os.getenv("OUTPOST_URL", "http://localhost:3333")

    if not admin_api_key:
        print("Please set the ADMIN_API_KEY environment variable.")
        sys.exit(1)

    sdk = Outpost(
        security=models.Security(admin_api_key=admin_api_key),
        server_url=f"{server_url}/api/v1",
    )

    sdk.tenants.upsert(tenant_id=tenant_id)

    print(
        """
You can create a topic or queue specific connection string with
send-only permissions using the Azure CLI.
Please replace $RESOURCE_GROUP, $NAMESPACE_NAME, and $TOPIC_NAME
with your actual values.

Create a send-only policy for the topic:
```bash
az servicebus topic authorization-rule create \\
  --resource-group $RESOURCE_GROUP \\
  --namespace-name $NAMESPACE_NAME \\
  --topic-name $TOPIC_NAME \\
  --name SendOnlyPolicy \\
  --rights Send
```

or for a queue:

```bash
az servicebus queue authorization-rule create \\
  --resource-group $RESOURCE_GROUP \\
  --namespace-name $NAMESPACE_NAME \\
  --queue-name $QUEUE_NAME \\
  --name SendOnlyPolicy \\
  --rights Send
```

Get the Topic-Specific Connection String:
```bash
az servicebus topic authorization-rule keys list \\
  --resource-group $RESOURCE_GROUP \\
  --namespace-name $NAMESPACE_NAME \\
  --topic-name $TOPIC_NAME \\
  --name SendOnlyPolicy \\
  --query primaryConnectionString \\
  --output tsv
```

or for a Queue-Specific Connection String:
```bash
az servicebus queue authorization-rule keys list \\
  --resource-group $RESOURCE_GROUP \\
  --namespace-name $NAMESPACE_NAME \\
  --queue-name $QUEUE_NAME \\
  --name SendOnlyPolicy \\
  --query primaryConnectionString \\
  --output tsv
```
"""
    )

    connection_string = questionary.text(
        "Enter Azure Service Bus Connection String:"
    ).ask()
    if connection_string is None:
        sys.exit(0)

    topic_or_queue_name = questionary.text(
        "Enter Azure Service Bus Topic or Queue name:"
    ).ask()
    if topic_or_queue_name is None:
        sys.exit(0)

    try:
        resp = sdk.destinations.create(
            tenant_id=tenant_id,
            destination_create=models.DestinationCreateAzureServiceBus(
                type=(models.DestinationCreateAzureServiceBusType.AZURE_SERVICEBUS),
                credentials=models.AzureServiceBusCredentials(
                    connection_string=connection_string
                ),
                config=models.AzureServiceBusConfig(name=topic_or_queue_name),
                topics="*",
            ),
        )

        if resp:
            print("Destination created successfully:", resp)
        else:
            print("Failed to create destination, response was empty.")

    except Exception as e:
        print(f"An error occurred: {e}")


if __name__ == "__main__":
    run()
