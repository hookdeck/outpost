# ListTenantsRequest


## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `Limit`                                                                  | **int64*                                                                 | :heavy_minus_sign:                                                       | Number of tenants to return per page (1-100, default 20).                |
| `Order`                                                                  | [*operations.Order](../../models/operations/order.md)                    | :heavy_minus_sign:                                                       | Sort order by `created_at` timestamp.                                    |
| `Next`                                                                   | **string*                                                                | :heavy_minus_sign:                                                       | Cursor for the next page of results. Mutually exclusive with `prev`.     |
| `Prev`                                                                   | **string*                                                                | :heavy_minus_sign:                                                       | Cursor for the previous page of results. Mutually exclusive with `next`. |