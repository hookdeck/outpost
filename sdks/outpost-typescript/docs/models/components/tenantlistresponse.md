# TenantListResponse

Paginated list of tenants.

## Example Usage

```typescript
import { TenantListResponse } from "@hookdeck/outpost-sdk/models/components";

let value: TenantListResponse = {
  data: [
    {
      id: "123",
      destinationsCount: 5,
      topics: [
        "user.created",
        "user.deleted",
      ],
      createdAt: new Date("2024-01-01T00:00:00Z"),
      updatedAt: new Date("2024-01-01T00:00:00Z"),
    },
  ],
  next: "MTcwNDA2NzIwMA==",
  prev: null,
  count: 42,
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              | Example                                                                  |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `data`                                                                   | [components.TenantListItem](../../models/components/tenantlistitem.md)[] | :heavy_minus_sign:                                                       | Array of tenant objects.                                                 |                                                                          |
| `next`                                                                   | *string*                                                                 | :heavy_minus_sign:                                                       | Cursor for the next page of results. Null if no more results.            | MTcwNDA2NzIwMA==                                                         |
| `prev`                                                                   | *string*                                                                 | :heavy_minus_sign:                                                       | Cursor for the previous page of results. Null if on first page.          | <nil>                                                                    |
| `count`                                                                  | *number*                                                                 | :heavy_minus_sign:                                                       | Total count of all tenants.                                              | 42                                                                       |