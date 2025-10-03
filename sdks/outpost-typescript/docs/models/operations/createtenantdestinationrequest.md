# CreateTenantDestinationRequest

## Example Usage

```typescript
import { CreateTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: CreateTenantDestinationRequest = {
  destinationCreate: {
    type: "webhook",
    topics: "*",
    config: {
      url: "https://example.com/webhooks/user",
    },
  },
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationCreate`                                                   | *components.DestinationCreate*                                        | :heavy_check_mark:                                                    | N/A                                                                   |