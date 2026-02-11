# EventFull

Full event object with data (returned when include=event.data).

## Example Usage

```typescript
import { EventFull } from "@hookdeck/outpost-sdk/models/components";

let value: EventFull = {
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
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `id`                                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           | evt_123                                                                                       |
| `tenantId`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | The tenant this event belongs to.                                                             | tnt_123                                                                                       |
| `destinationId`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | The destination this event was delivered to.                                                  | des_456                                                                                       |
| `topic`                                                                                       | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           | user.created                                                                                  |
| `time`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Time the event was received.                                                                  | 2024-01-01T00:00:00Z                                                                          |
| `eligibleForRetry`                                                                            | *boolean*                                                                                     | :heavy_minus_sign:                                                                            | Whether this event can be retried.                                                            | true                                                                                          |
| `metadata`                                                                                    | Record<string, *string*>                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           | {<br/>"source": "crm"<br/>}                                                                   |
| `data`                                                                                        | Record<string, *any*>                                                                         | :heavy_minus_sign:                                                                            | The event payload data.                                                                       | {<br/>"user_id": "userid",<br/>"status": "active"<br/>}                                       |