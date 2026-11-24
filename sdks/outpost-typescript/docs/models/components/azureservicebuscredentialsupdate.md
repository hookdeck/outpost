# AzureServiceBusCredentialsUpdate

Partial Azure Service Bus credentials for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { AzureServiceBusCredentialsUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: AzureServiceBusCredentialsUpdate = {};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `connectionString`                                         | *string*                                                   | :heavy_minus_sign:                                         | The connection string for the Azure Service Bus namespace. |