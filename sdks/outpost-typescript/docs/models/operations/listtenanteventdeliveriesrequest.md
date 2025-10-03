# ListTenantEventDeliveriesRequest

## Example Usage

```typescript
import { ListTenantEventDeliveriesRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantEventDeliveriesRequest = {
  eventId: "<id>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `eventId`                                                             | *string*                                                              | :heavy_check_mark:                                                    | The ID of the event.                                                  |