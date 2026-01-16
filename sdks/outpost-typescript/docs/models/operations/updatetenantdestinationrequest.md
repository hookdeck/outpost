# UpdateTenantDestinationRequest

## Example Usage

```typescript
import { UpdateTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: UpdateTenantDestinationRequest = {
  destinationId: "<id>",
  params: {
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
    credentials: {
      token: "hd_token_...",
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
| `tenantId`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationId`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `params`                                                              | *components.DestinationUpdate*                                        | :heavy_check_mark:                                                    | N/A                                                                   |