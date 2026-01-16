# ListTenantsRequest


## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `limit`                                                                  | *Optional[int]*                                                          | :heavy_minus_sign:                                                       | Number of tenants to return per page (1-100, default 20).                |
| `order`                                                                  | [Optional[models.Order]](../models/order.md)                             | :heavy_minus_sign:                                                       | Sort order by `created_at` timestamp.                                    |
| `next_cursor`                                                            | *Optional[str]*                                                          | :heavy_minus_sign:                                                       | Cursor for the next page of results. Mutually exclusive with `prev`.     |
| `prev_cursor`                                                            | *Optional[str]*                                                          | :heavy_minus_sign:                                                       | Cursor for the previous page of results. Mutually exclusive with `next`. |