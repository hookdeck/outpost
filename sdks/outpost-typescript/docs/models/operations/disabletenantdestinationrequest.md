# DisableTenantDestinationRequest

## Example Usage

```typescript
import { DisableTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: DisableTenantDestinationRequest = {
  destinationId: "<id>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationId`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |