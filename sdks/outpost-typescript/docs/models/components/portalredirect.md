# PortalRedirect

## Example Usage

```typescript
import { PortalRedirect } from "@hookdeck/outpost-sdk/models/components";

let value: PortalRedirect = {
  redirectUrl:
    "https://webhooks.acme.com/?token=JWT_TOKEN&tenant_id=tenant_123",
  tenantId: "tenant_123",
};
```

## Fields

| Field                                                                   | Type                                                                    | Required                                                                | Description                                                             | Example                                                                 |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `redirectUrl`                                                           | *string*                                                                | :heavy_minus_sign:                                                      | Redirect URL containing a JWT to authenticate the user with the portal. | https://webhooks.acme.com/?token=JWT_TOKEN&tenant_id=tenant_123         |
| `tenantId`                                                              | *string*                                                                | :heavy_minus_sign:                                                      | The ID of the tenant associated with this portal session.               | tenant_123                                                              |