# UpsertTenantRequest


## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `TenantID`                                                            | `string`                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `Body`                                                                | [*components.TenantUpsert](../../models/components/tenantupsert.md)   | :heavy_minus_sign:                                                    | Optional tenant metadata                                              |