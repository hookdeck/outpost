# TenantToken


## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                | Example                                                    |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `token`                                                    | *Optional[str]*                                            | :heavy_minus_sign:                                         | JWT token scoped to the tenant for safe browser API calls. | SOME_JWT_TOKEN                                             |
| `tenant_id`                                                | *Optional[str]*                                            | :heavy_minus_sign:                                         | The ID of the tenant this token is scoped to.              | tenant_123                                                 |