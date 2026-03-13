# ListAttemptsResponse

## Example Usage

```typescript
import { ListAttemptsResponse } from "@hookdeck/outpost-sdk/models/operations";

let value: ListAttemptsResponse = {
  result: {
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
  },
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `result`                                                                               | [components.AttemptPaginatedResult](../../models/components/attemptpaginatedresult.md) | :heavy_check_mark:                                                                     | N/A                                                                                    |