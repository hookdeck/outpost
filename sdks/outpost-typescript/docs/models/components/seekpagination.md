# SeekPagination

Cursor-based pagination metadata for list responses.

## Example Usage

```typescript
import { SeekPagination } from "@hookdeck/outpost-sdk/models/components";

let value: SeekPagination = {
  orderBy: "created_at",
  dir: "desc",
  limit: 100,
  next: "MTcwNDA2NzIwMA==",
  prev: null,
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     | Example                                                         |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `orderBy`                                                       | *string*                                                        | :heavy_minus_sign:                                              | The field being sorted on.                                      | created_at                                                      |
| `dir`                                                           | [components.Dir](../../models/components/dir.md)                | :heavy_minus_sign:                                              | Sort direction.                                                 | desc                                                            |
| `limit`                                                         | *number*                                                        | :heavy_minus_sign:                                              | Page size limit.                                                | 100                                                             |
| `next`                                                          | *string*                                                        | :heavy_minus_sign:                                              | Cursor for the next page of results. Null if no more results.   | MTcwNDA2NzIwMA==                                                |
| `prev`                                                          | *string*                                                        | :heavy_minus_sign:                                              | Cursor for the previous page of results. Null if on first page. | <nil>                                                           |