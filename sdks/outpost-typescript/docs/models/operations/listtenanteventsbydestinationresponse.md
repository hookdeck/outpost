# ListTenantEventsByDestinationResponse

A paginated list of events for the destination.

## Example Usage

```typescript
import { ListTenantEventsByDestinationResponse } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantEventsByDestinationResponse = {
  count: 42,
  data: [
    {
      id: "evt_123",
      destinationId: "des_456",
      topic: "user.created",
      time: new Date("2024-01-01T00:00:00Z"),
      successfulAt: new Date("2024-01-01T00:00:00Z"),
      metadata: {
        "source": "crm",
      },
      status: "success",
      data: {
        "user_id": "userid",
        "status": "active",
      },
    },
  ],
  next: "",
  prev: "",
};
```

## Fields

| Field                                                       | Type                                                        | Required                                                    | Description                                                 | Example                                                     |
| ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- |
| `count`                                                     | *number*                                                    | :heavy_check_mark:                                          | Total number of items across all pages                      | 42                                                          |
| `data`                                                      | [components.Event](../../models/components/event.md)[]      | :heavy_check_mark:                                          | N/A                                                         |                                                             |
| `next`                                                      | *string*                                                    | :heavy_check_mark:                                          | Cursor for next page (empty string if no next page)         |                                                             |
| `prev`                                                      | *string*                                                    | :heavy_check_mark:                                          | Cursor for previous page (empty string if no previous page) |                                                             |