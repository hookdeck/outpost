# EnableTenantDestinationRequest

## Example Usage

```typescript
import { EnableTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: EnableTenantDestinationRequest = {
  tenantId: "<id>",
  destinationId: "<id>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationId`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |