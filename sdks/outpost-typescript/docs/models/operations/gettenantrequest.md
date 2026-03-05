# GetTenantRequest

## Example Usage

```typescript
import { GetTenantRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: GetTenantRequest = {
  tenantId: "<id>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |