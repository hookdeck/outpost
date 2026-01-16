# UpsertTenantRequest


## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `params`                                                              | [Optional[models.TenantUpsert]](../models/tenantupsert.md)            | :heavy_minus_sign:                                                    | Optional tenant metadata                                              |