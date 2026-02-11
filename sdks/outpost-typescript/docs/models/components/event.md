# Event

## Example Usage

```typescript
import { Event } from "@hookdeck/outpost-sdk/models/components";

let value: Event = {
  id: "evt_123",
  tenantId: "tnt_123",
  destinationId: "des_456",
  topic: "user.created",
  time: new Date("2024-01-01T00:00:00Z"),
  successfulAt: new Date("2024-01-01T00:00:00Z"),
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
| `destinationId`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           | des_456                                                                                       |
| `topic`                                                                                       | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           | user.created                                                                                  |
| `time`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Time the event was received/processed.                                                        | 2024-01-01T00:00:00Z                                                                          |
| `successfulAt`                                                                                | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Time the event was successfully delivered.                                                    | 2024-01-01T00:00:00Z                                                                          |
| `metadata`                                                                                    | Record<string, *string*>                                                                      | :heavy_minus_sign:                                                                            | Key-value string pairs of metadata associated with the event.                                 | {<br/>"source": "crm"<br/>}                                                                   |
| `data`                                                                                        | Record<string, *any*>                                                                         | :heavy_minus_sign:                                                                            | Freeform JSON data of the event.                                                              | {<br/>"user_id": "userid",<br/>"status": "active"<br/>}                                       |