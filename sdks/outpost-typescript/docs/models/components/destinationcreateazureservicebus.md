# DestinationCreateAzureServiceBus

## Example Usage

```typescript
import { DestinationCreateAzureServiceBus } from "@hookdeck/outpost-sdk/models/components";

let value: DestinationCreateAzureServiceBus = {
  id: "user-provided-id",
  type: "azure_servicebus",
  topics: "*",
  config: {
    name: "my-queue-or-topic",
  },
  credentials: {
    connectionString:
      "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=abc123",
  },
};
```

## Fields

| Field                                                                                                              | Type                                                                                                               | Required                                                                                                           | Description                                                                                                        | Example                                                                                                            |
| ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| `id`                                                                                                               | *string*                                                                                                           | :heavy_minus_sign:                                                                                                 | Optional user-provided ID. A UUID will be generated if empty.                                                      | user-provided-id                                                                                                   |
| `type`                                                                                                             | [components.DestinationCreateAzureServiceBusType](../../models/components/destinationcreateazureservicebustype.md) | :heavy_check_mark:                                                                                                 | Type of the destination. Must be 'azure_servicebus'.                                                               |                                                                                                                    |
| `topics`                                                                                                           | *components.Topics*                                                                                                | :heavy_check_mark:                                                                                                 | "*" or an array of enabled topics.                                                                                 | *                                                                                                                  |
| `config`                                                                                                           | [components.AzureServiceBusConfig](../../models/components/azureservicebusconfig.md)                               | :heavy_check_mark:                                                                                                 | N/A                                                                                                                |                                                                                                                    |
| `credentials`                                                                                                      | [components.AzureServiceBusCredentials](../../models/components/azureservicebuscredentials.md)                     | :heavy_check_mark:                                                                                                 | N/A                                                                                                                |                                                                                                                    |