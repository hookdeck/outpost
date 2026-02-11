# SeekPagination

Cursor-based pagination metadata for list responses.


## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     | Example                                                         |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `OrderBy`                                                       | **string*                                                       | :heavy_minus_sign:                                              | The field being sorted on.                                      | created_at                                                      |
| `Dir`                                                           | [*components.Dir](../../models/components/dir.md)               | :heavy_minus_sign:                                              | Sort direction.                                                 | desc                                                            |
| `Limit`                                                         | **int64*                                                        | :heavy_minus_sign:                                              | Page size limit.                                                | 100                                                             |
| `Next`                                                          | **string*                                                       | :heavy_minus_sign:                                              | Cursor for the next page of results. Null if no more results.   | MTcwNDA2NzIwMA==                                                |
| `Prev`                                                          | **string*                                                       | :heavy_minus_sign:                                              | Cursor for the previous page of results. Null if on first page. | <nil>                                                           |