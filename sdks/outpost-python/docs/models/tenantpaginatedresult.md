# TenantPaginatedResult

Paginated list of tenants.


## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    | Example                                                        |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `models`                                                       | List[[models.Tenant](../models/tenant.md)]                     | :heavy_minus_sign:                                             | Array of tenant objects.                                       |                                                                |
| `pagination`                                                   | [Optional[models.SeekPagination]](../models/seekpagination.md) | :heavy_minus_sign:                                             | Cursor-based pagination metadata for list responses.           |                                                                |
| `count`                                                        | *Optional[int]*                                                | :heavy_minus_sign:                                             | Total count of all tenants.                                    | 42                                                             |