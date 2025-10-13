# DestinationUpdateAzureServiceBus

## Example Usage

```typescript
import { DestinationUpdateAzureServiceBus } from "@hookdeck/outpost-sdk/models/components";

let value: DestinationUpdateAzureServiceBus = {
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

| Field                                                                                          | Type                                                                                           | Required                                                                                       | Description                                                                                    | Example                                                                                        |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `topics`                                                                                       | *components.Topics*                                                                            | :heavy_minus_sign:                                                                             | "*" or an array of enabled topics.                                                             | *                                                                                              |
| `config`                                                                                       | [components.AzureServiceBusConfig](../../models/components/azureservicebusconfig.md)           | :heavy_minus_sign:                                                                             | N/A                                                                                            |                                                                                                |
| `credentials`                                                                                  | [components.AzureServiceBusCredentials](../../models/components/azureservicebuscredentials.md) | :heavy_minus_sign:                                                                             | N/A                                                                                            |                                                                                                |