# EventPaginatedResult

Paginated list of events.

## Example Usage

```typescript
import { EventPaginatedResult } from "@hookdeck/outpost-sdk/models/components";

let value: EventPaginatedResult = {
  pagination: {
    orderBy: "created_at",
    dir: "desc",
    limit: 100,
    next: "MTcwNDA2NzIwMA==",
    prev: null,
  },
  models: [
    {
      id: "evt_123",
      tenantId: "tnt_123",
      matchedDestinationIds: [
        "des_456",
        "des_789",
      ],
      topic: "user.created",
      time: new Date("2024-01-01T00:00:00Z"),
      metadata: {
        "source": "crm",
      },
      data: {
        "user_id": "userid",
        "status": "active",
      },
    },
  ],
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `pagination`                                                           | [components.SeekPagination](../../models/components/seekpagination.md) | :heavy_minus_sign:                                                     | Cursor-based pagination metadata for list responses.                   |
| `models`                                                               | [components.Event](../../models/components/event.md)[]                 | :heavy_minus_sign:                                                     | Array of event objects.                                                |