# PublishRequest

## Example Usage

```typescript
import { PublishRequest } from "@hookdeck/outpost-sdk/models/components";

let value: PublishRequest = {
  id: "evt_custom_123",
  tenantId: "<TENANT_ID>",
  destinationId: "<DESTINATION_ID>",
  topic: "topic.name",
  time: new Date("2024-01-15T10:30:00Z"),
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
| `id`                                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | Optional. A unique identifier for the event. If not provided, a UUID will be generated.       | evt_custom_123                                                                                |
| `tenantId`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the tenant to publish for.                                                          | <TENANT_ID>                                                                                   |
| `destinationId`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Optional. Route event to a specific destination.                                              | <DESTINATION_ID>                                                                              |
| `topic`                                                                                       | *string*                                                                                      | :heavy_minus_sign:                                                                            | Topic name for the event. Required if Outpost has been configured with topics.                | topic.name                                                                                    |
| `eligibleForRetry`                                                                            | *boolean*                                                                                     | :heavy_minus_sign:                                                                            | Should event delivery be retried on failure.                                                  |                                                                                               |
| `time`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Optional. Custom timestamp for the event. If not provided, defaults to the current time.      | 2024-01-15T10:30:00Z                                                                          |
| `metadata`                                                                                    | Record<string, *string*>                                                                      | :heavy_minus_sign:                                                                            | Any key-value string pairs for metadata.                                                      | {<br/>"source": "crm"<br/>}                                                                   |
| `data`                                                                                        | Record<string, *any*>                                                                         | :heavy_check_mark:                                                                            | Any JSON payload for the event data.                                                          | {<br/>"user_id": "userid",<br/>"status": "active"<br/>}                                       |