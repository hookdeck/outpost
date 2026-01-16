# UpsertTenantRequest

## Example Usage

```typescript
import { UpsertTenantRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: UpsertTenantRequest = {};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `params`                                                              | [components.TenantUpsert](../../models/components/tenantupsert.md)    | :heavy_minus_sign:                                                    | Optional tenant metadata                                              |