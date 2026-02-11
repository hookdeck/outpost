# RetryRequest

Request body for retrying event delivery to a destination.

## Example Usage

```typescript
import { RetryRequest } from "@hookdeck/outpost-sdk/models/components";

let value: RetryRequest = {
  eventId: "evt_123",
  destinationId: "des_456",
};
```

## Fields

| Field                                    | Type                                     | Required                                 | Description                              | Example                                  |
| ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- |
| `eventId`                                | *string*                                 | :heavy_check_mark:                       | The ID of the event to retry.            | evt_123                                  |
| `destinationId`                          | *string*                                 | :heavy_check_mark:                       | The ID of the destination to deliver to. | des_456                                  |