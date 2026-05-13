# GetTenantPortalURLRequest


## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `theme`                                                               | [Optional[models.Theme]](../models/theme.md)                          | :heavy_minus_sign:                                                    | Optional theme preference for the portal.                             |