# ListEventsResponse

## Example Usage

```typescript
import { ListEventsResponse } from "@hookdeck/outpost-sdk/models/operations";

let value: ListEventsResponse = {
  result: {
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
    pagination: {
      orderBy: "created_at",
      dir: "desc",
      limit: 100,
      next: "MTcwNDA2NzIwMA==",
      prev: null,
    },
  },
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `result`                                                                           | [components.EventPaginatedResult](../../models/components/eventpaginatedresult.md) | :heavy_check_mark:                                                                 | N/A                                                                                |