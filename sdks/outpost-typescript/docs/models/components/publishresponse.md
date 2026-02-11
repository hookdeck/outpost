# PublishResponse

## Example Usage

```typescript
import { PublishResponse } from "@hookdeck/outpost-sdk/models/components";

let value: PublishResponse = {
  id: "evt_abc123xyz789",
  duplicate: false,
  destinationIds: [
    "des_456",
    "des_789",
  ],
};
```

## Fields

| Field                                                                                                                                                              | Type                                                                                                                                                               | Required                                                                                                                                                           | Description                                                                                                                                                        | Example                                                                                                                                                            |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `id`                                                                                                                                                               | *string*                                                                                                                                                           | :heavy_check_mark:                                                                                                                                                 | The ID of the event that was accepted for publishing. This will be the ID provided in the request's `id` field if present, otherwise it's a server-generated UUID. | evt_abc123xyz789                                                                                                                                                   |
| `duplicate`                                                                                                                                                        | *boolean*                                                                                                                                                          | :heavy_check_mark:                                                                                                                                                 | Whether this event was already processed (idempotency hit). If true, the event was not queued again.                                                               | false                                                                                                                                                              |
| `destinationIds`                                                                                                                                                   | *string*[]                                                                                                                                                         | :heavy_check_mark:                                                                                                                                                 | The IDs of destinations that matched this event. Empty array if no destinations matched.                                                                           | [<br/>"des_456",<br/>"des_789"<br/>]                                                                                                                               |