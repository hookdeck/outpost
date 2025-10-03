# ListTenantEventsByDestinationResponse

## Example Usage

```typescript
import { ListTenantEventsByDestinationResponse } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantEventsByDestinationResponse = {
  result: {
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
        data: {
          "user_id": "userid",
          "status": "active",
        },
      },
    ],
    nextCursor: "",
    prevCursor: "",
  },
};
```

## Fields

| Field                                                                                                                        | Type                                                                                                                         | Required                                                                                                                     | Description                                                                                                                  |
| ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `result`                                                                                                                     | [operations.ListTenantEventsByDestinationResponseBody](../../models/operations/listtenanteventsbydestinationresponsebody.md) | :heavy_check_mark:                                                                                                           | N/A                                                                                                                          |