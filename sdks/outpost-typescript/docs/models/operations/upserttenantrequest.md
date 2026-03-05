# UpsertTenantRequest

## Example Usage

```typescript
import { UpsertTenantRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: UpsertTenantRequest = {
  tenantId: "<id>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `params`                                                              | [components.TenantUpsert](../../models/components/tenantupsert.md)    | :heavy_minus_sign:                                                    | Optional tenant metadata                                              |