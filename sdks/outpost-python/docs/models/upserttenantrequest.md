# UpsertTenantRequest


## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `body`                                                                | [Optional[models.TenantUpsert]](../models/tenantupsert.md)            | :heavy_minus_sign:                                                    | Optional tenant metadata                                              |