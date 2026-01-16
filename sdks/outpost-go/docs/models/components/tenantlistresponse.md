# TenantListResponse

Paginated list of tenants.


## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              | Example                                                                  |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `Data`                                                                   | [][components.TenantListItem](../../models/components/tenantlistitem.md) | :heavy_minus_sign:                                                       | Array of tenant objects.                                                 |                                                                          |
| `Next`                                                                   | **string*                                                                | :heavy_minus_sign:                                                       | Cursor for the next page of results. Null if no more results.            | MTcwNDA2NzIwMA==                                                         |
| `Prev`                                                                   | **string*                                                                | :heavy_minus_sign:                                                       | Cursor for the previous page of results. Null if on first page.          | <nil>                                                                    |
| `Count`                                                                  | **int64*                                                                 | :heavy_minus_sign:                                                       | Total count of all tenants.                                              | 42                                                                       |