# GetTenantPortalURLRequest


## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `TenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `Theme`                                                               | [*operations.Theme](../../models/operations/theme.md)                 | :heavy_minus_sign:                                                    | Optional theme preference for the portal.                             |