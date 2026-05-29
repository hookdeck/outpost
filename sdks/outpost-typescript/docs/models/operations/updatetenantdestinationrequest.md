# UpdateTenantDestinationRequest

## Example Usage

```typescript
import { UpdateTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: UpdateTenantDestinationRequest = {
  tenantId: "<id>",
  destinationId: "<id>",
  body: {
    type: "hookdeck",
  },
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationId`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `body`                                                                | *components.DestinationUpdate*                                        | :heavy_check_mark:                                                    | N/A                                                                   |