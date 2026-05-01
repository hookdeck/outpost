# UpdateTenantDestinationRequest

## Example Usage

```typescript
import { UpdateTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: UpdateTenantDestinationRequest = {
  tenantId: "<id>",
  destinationId: "<id>",
  body: {
    topics: "*",
    filter: {
      "data": {
        "amount": {
          "$gte": 100,
        },
        "customer": {
          "tier": "premium",
        },
      },
    },
    config: {
      streamName: "my-data-stream",
      region: "us-east-1",
      endpoint: "https://kinesis.us-east-1.amazonaws.com",
      partitionKeyTemplate: "data.\"user_id\"",
    },
    credentials: {
      key: "AKIAIOSFODNN7EXAMPLE",
      secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
      session: "AQoDYXdzEPT//////////wEXAMPLE...",
    },
    deliveryMetadata: {
      "app-id": "my-app",
      "region": "us-east-1",
    },
    metadata: {
      "internal-id": "123",
      "team": "platform",
    },
  },
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationId`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `body`                                                                | *components.DestinationUpdate*                                        | :heavy_check_mark:                                                    | N/A                                                                   |