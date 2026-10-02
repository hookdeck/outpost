# AttemptPaginatedResult

Paginated list of attempts.

## Example Usage

```typescript
import { AttemptPaginatedResult } from "@hookdeck/outpost-sdk/models/components";

let value: AttemptPaginatedResult = {
  pagination: {
    orderBy: "created_at",
    dir: "desc",
    limit: 100,
    next: "MTcwNDA2NzIwMA==",
    prev: null,
  },
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
      latencyMs: 212,
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
      destination: {
        id: "des_12345",
        tenantId: "tenant_123",
        type: "webhook",
        topics: "*",
        filter: {
          "data": {
            "amount": {
              "$gte": 100,
            },
            "customer": {
              "tier": "premium",
            },
          },
        },
        config: {
          "url": "https://my-service.com/webhook/handler",
        },
        deliveryMetadata: {
          "app-id": "my-app",
          "region": "us-east-1",
        },
        metadata: {
          "internal-id": "123",
          "team": "platform",
        },
        disabledAt: null,
        createdAt: new Date("2024-01-01T00:00:00Z"),
        updatedAt: new Date("2024-01-01T00:00:00Z"),
        target: "my-service.com",
        targetUrl: "https://my-service.com/webhook/handler",
      },
    },
  ],
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `pagination`                                                           | [components.SeekPagination](../../models/components/seekpagination.md) | :heavy_minus_sign:                                                     | Cursor-based pagination metadata for list responses.                   |
| `models`                                                               | [components.Attempt](../../models/components/attempt.md)[]             | :heavy_minus_sign:                                                     | Array of attempt objects.                                              |