# DeleteTenantDestinationRequest

## Example Usage

```typescript
import { DeleteTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: DeleteTenantDestinationRequest = {
  tenantId: "<id>",
  destinationId: "<id>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationId`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |