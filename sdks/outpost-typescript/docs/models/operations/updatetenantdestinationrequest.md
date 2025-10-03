# UpdateTenantDestinationRequest

## Example Usage

```typescript
import { UpdateTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: UpdateTenantDestinationRequest = {
  destinationId: "<id>",
  destinationUpdate: {
    topics: "*",
    config: {
      serverUrl: "localhost:5672",
      exchange: "my-exchange",
      tls: "false",
    },
    credentials: {
      username: "guest",
      password: "guest",
    },
  },
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationId`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `destinationUpdate`                                                   | *components.DestinationUpdate*                                        | :heavy_check_mark:                                                    | N/A                                                                   |