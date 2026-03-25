# Attempt

An attempt represents a single delivery attempt of an event to a destination.

## Example Usage

```typescript
import { Attempt } from "@hookdeck/outpost-sdk/models/components";

let value: Attempt = {
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
  destination: {
    id: "des_webhook_123",
    type: "webhook",
    topics: [
      "user.created",
      "order.shipped",
    ],
    disabledAt: null,
    createdAt: new Date("2024-02-15T10:00:00Z"),
    updatedAt: new Date("2024-02-15T10:00:00Z"),
    config: {
      url: "https://my-service.com/webhook/handler",
    },
    credentials: {
      secret: "whsec_abc123def456",
      previousSecret: "whsec_prev789xyz012",
      previousSecretInvalidAt: new Date("2024-02-16T10:00:00Z"),
    },
  },
};
```

## Fields

| Field                                                                                                    | Type                                                                                                     | Required                                                                                                 | Description                                                                                              | Example                                                                                                  |
| -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `id`                                                                                                     | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | Unique identifier for this attempt.                                                                      | atm_123                                                                                                  |
| `tenantId`                                                                                               | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | The tenant this attempt belongs to.                                                                      | tnt_123                                                                                                  |
| `status`                                                                                                 | [components.Status](../../models/components/status.md)                                                   | :heavy_minus_sign:                                                                                       | The attempt status.                                                                                      | success                                                                                                  |
| `time`                                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)            | :heavy_minus_sign:                                                                                       | Time the attempt was made.                                                                               | 2024-01-01T00:00:05Z                                                                                     |
| `code`                                                                                                   | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | Response status code or error code.                                                                      | 200                                                                                                      |
| `responseData`                                                                                           | Record<string, *any*>                                                                                    | :heavy_minus_sign:                                                                                       | Response data from the attempt. Only included when include=response_data.                                | {<br/>"status_code": 200,<br/>"body": "{\"status\":\"ok\"}",<br/>"headers": {<br/>"content-type": "application/json"<br/>}<br/>} |
| `attemptNumber`                                                                                          | *number*                                                                                                 | :heavy_minus_sign:                                                                                       | The attempt number (1 for first attempt, 2+ for retries).                                                | 1                                                                                                        |
| `manual`                                                                                                 | *boolean*                                                                                                | :heavy_minus_sign:                                                                                       | Whether this attempt was manually triggered (e.g., a retry initiated by a user).                         | false                                                                                                    |
| `eventId`                                                                                                | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | The ID of the associated event.                                                                          | evt_123                                                                                                  |
| `destinationId`                                                                                          | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | The destination ID this attempt was sent to.                                                             | des_456                                                                                                  |
| `event`                                                                                                  | *components.EventUnion*                                                                                  | :heavy_minus_sign:                                                                                       | The associated event object. Only present when include=event or include=event.data.                      |                                                                                                          |
| `destination`                                                                                            | *components.Destination*                                                                                 | :heavy_minus_sign:                                                                                       | N/A                                                                                                      |                                                                                                          |