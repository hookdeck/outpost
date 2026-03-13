# ListTenantsResponse

## Example Usage

```typescript
import { ListTenantsResponse } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantsResponse = {
  result: {
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
  },
};
```

## Fields

| Field                                                                                | Type                                                                                 | Required                                                                             | Description                                                                          |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `result`                                                                             | [components.TenantPaginatedResult](../../models/components/tenantpaginatedresult.md) | :heavy_check_mark:                                                                   | N/A                                                                                  |