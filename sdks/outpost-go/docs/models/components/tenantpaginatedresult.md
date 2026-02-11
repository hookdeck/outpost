# TenantPaginatedResult

Paginated list of tenants.


## Fields

| Field                                                                   | Type                                                                    | Required                                                                | Description                                                             | Example                                                                 |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `Models`                                                                | [][components.Tenant](../../models/components/tenant.md)                | :heavy_minus_sign:                                                      | Array of tenant objects.                                                |                                                                         |
| `Pagination`                                                            | [*components.SeekPagination](../../models/components/seekpagination.md) | :heavy_minus_sign:                                                      | Cursor-based pagination metadata for list responses.                    |                                                                         |
| `Count`                                                                 | **int64*                                                                | :heavy_minus_sign:                                                      | Total count of all tenants.                                             | 42                                                                      |