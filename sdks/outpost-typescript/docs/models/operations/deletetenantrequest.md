# DeleteTenantRequest

## Example Usage

```typescript
import { DeleteTenantRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: DeleteTenantRequest = {
  tenantId: "<id>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |