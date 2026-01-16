# PortalRedirect


## Fields

| Field                                                                   | Type                                                                    | Required                                                                | Description                                                             | Example                                                                 |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `RedirectURL`                                                           | **string*                                                               | :heavy_minus_sign:                                                      | Redirect URL containing a JWT to authenticate the user with the portal. | https://webhooks.acme.com/?token=JWT_TOKEN&tenant_id=tenant_123         |
| `TenantID`                                                              | **string*                                                               | :heavy_minus_sign:                                                      | The ID of the tenant associated with this portal session.               | tenant_123                                                              |