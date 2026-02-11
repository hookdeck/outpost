# AttemptPaginatedResult

Paginated list of attempts.

## Example Usage

```typescript
import { AttemptPaginatedResult } from "@hookdeck/outpost-sdk/models/components";

let value: AttemptPaginatedResult = {
  models: [
    {
      id: "atm_123",
      tenantId: "tnt_123",
      status: "success",
      time: new Date("2024-01-01T00:00:05Z"),
      code: "200",
      responseData: {
        "status_code": 200,
        "body": "{\"status\":\"ok\"}",
        "headers": {
          "content-type": "application/json",
        },
      },
      attemptNumber: 1,
      manual: false,
      eventId: "evt_123",
      destinationId: "des_456",
      event: {
        id: "evt_123",
        tenantId: "tnt_123",
        destinationId: "des_456",
        topic: "user.created",
        time: new Date("2024-01-01T00:00:00Z"),
        eligibleForRetry: true,
        metadata: {
          "source": "crm",
        },
        data: {
          "user_id": "userid",
          "status": "active",
        },
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
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `models`                                                               | [components.Attempt](../../models/components/attempt.md)[]             | :heavy_minus_sign:                                                     | Array of attempt objects.                                              |
| `pagination`                                                           | [components.SeekPagination](../../models/components/seekpagination.md) | :heavy_minus_sign:                                                     | Cursor-based pagination metadata for list responses.                   |