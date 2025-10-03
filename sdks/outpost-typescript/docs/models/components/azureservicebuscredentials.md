# AzureServiceBusCredentials

## Example Usage

```typescript
import { AzureServiceBusCredentials } from "@hookdeck/outpost-sdk/models/components";

let value: AzureServiceBusCredentials = {
  connectionString:
    "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=abc123",
};
```

## Fields

| Field                                                                                                                | Type                                                                                                                 | Required                                                                                                             | Description                                                                                                          | Example                                                                                                              |
| -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `connectionString`                                                                                                   | *string*                                                                                                             | :heavy_check_mark:                                                                                                   | The connection string for the Azure Service Bus namespace.                                                           | Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=abc123 |