# TenantListResponse

Paginated list of tenants.


## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     | Example                                                         |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `data`                                                          | List[[models.TenantListItem](../models/tenantlistitem.md)]      | :heavy_minus_sign:                                              | Array of tenant objects.                                        |                                                                 |
| `next`                                                          | *OptionalNullable[str]*                                         | :heavy_minus_sign:                                              | Cursor for the next page of results. Null if no more results.   | MTcwNDA2NzIwMA==                                                |
| `prev`                                                          | *OptionalNullable[str]*                                         | :heavy_minus_sign:                                              | Cursor for the previous page of results. Null if on first page. | <nil>                                                           |
| `count`                                                         | *Optional[int]*                                                 | :heavy_minus_sign:                                              | Total count of all tenants.                                     | 42                                                              |