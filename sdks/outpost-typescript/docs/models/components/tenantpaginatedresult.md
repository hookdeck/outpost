# TenantPaginatedResult

Paginated list of tenants.

## Example Usage

```typescript
import { TenantPaginatedResult } from "@hookdeck/outpost-sdk/models/components";

let value: TenantPaginatedResult = {
  models: [
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
  pagination: {
    orderBy: "created_at",
    dir: "desc",
    limit: 100,
    next: "MTcwNDA2NzIwMA==",
    prev: null,
  },
  count: 42,
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            | Example                                                                |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `models`                                                               | [components.Tenant](../../models/components/tenant.md)[]               | :heavy_minus_sign:                                                     | Array of tenant objects.                                               |                                                                        |
| `pagination`                                                           | [components.SeekPagination](../../models/components/seekpagination.md) | :heavy_minus_sign:                                                     | Cursor-based pagination metadata for list responses.                   |                                                                        |
| `count`                                                                | *number*                                                               | :heavy_minus_sign:                                                     | Total count of all tenants.                                            | 42                                                                     |