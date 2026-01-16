# UpsertTenantRequest


## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `TenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `Params`                                                              | [*components.TenantUpsert](../../models/components/tenantupsert.md)   | :heavy_minus_sign:                                                    | Optional tenant metadata                                              |