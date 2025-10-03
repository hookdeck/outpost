# GetTenantPortalUrlRequest

## Example Usage

```typescript
import { GetTenantPortalUrlRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: GetTenantPortalUrlRequest = {};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `theme`                                                               | [operations.Theme](../../models/operations/theme.md)                  | :heavy_minus_sign:                                                    | Optional theme preference for the portal.                             |