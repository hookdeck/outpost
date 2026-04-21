# GetEventRequest

## Example Usage

```typescript
import { GetEventRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: GetEventRequest = {
  eventId: "<id>",
};
```

## Fields

| Field                                                                                                                                | Type                                                                                                                                 | Required                                                                                                                             | Description                                                                                                                          |
| ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ |
| `eventId`                                                                                                                            | *string*                                                                                                                             | :heavy_check_mark:                                                                                                                   | The ID of the event.                                                                                                                 |
| `tenantId`                                                                                                                           | *string*                                                                                                                             | :heavy_minus_sign:                                                                                                                   | Filter by tenant ID. Returns 404 if the event does not belong to the specified tenant. Ignored when using Tenant JWT authentication. |