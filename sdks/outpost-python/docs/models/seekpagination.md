# SeekPagination

Cursor-based pagination metadata for list responses.


## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     | Example                                                         |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `order_by`                                                      | *Optional[str]*                                                 | :heavy_minus_sign:                                              | The field being sorted on.                                      | created_at                                                      |
| `dir`                                                           | [Optional[models.Dir]](../models/dir.md)                        | :heavy_minus_sign:                                              | Sort direction.                                                 | desc                                                            |
| `limit`                                                         | *Optional[int]*                                                 | :heavy_minus_sign:                                              | Page size limit.                                                | 100                                                             |
| `next_cursor`                                                   | *OptionalNullable[str]*                                         | :heavy_minus_sign:                                              | Cursor for the next page of results. Null if no more results.   | MTcwNDA2NzIwMA==                                                |
| `prev_cursor`                                                   | *OptionalNullable[str]*                                         | :heavy_minus_sign:                                              | Cursor for the previous page of results. Null if on first page. | <nil>                                                           |