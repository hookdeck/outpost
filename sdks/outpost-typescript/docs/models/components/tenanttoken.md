# TenantToken

## Example Usage

```typescript
import { TenantToken } from "@hookdeck/outpost-sdk/models/components";

let value: TenantToken = {
  token: "SOME_JWT_TOKEN",
  tenantId: "tenant_123",
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                | Example                                                    |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `token`                                                    | *string*                                                   | :heavy_minus_sign:                                         | JWT token scoped to the tenant for safe browser API calls. | SOME_JWT_TOKEN                                             |
| `tenantId`                                                 | *string*                                                   | :heavy_minus_sign:                                         | The ID of the tenant this token is scoped to.              | tenant_123                                                 |