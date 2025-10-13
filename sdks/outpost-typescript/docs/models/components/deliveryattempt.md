# DeliveryAttempt

## Example Usage

```typescript
import { DeliveryAttempt } from "@hookdeck/outpost-sdk/models/components";

let value: DeliveryAttempt = {
  deliveredAt: new Date("2024-01-01T00:00:00Z"),
  status: "success",
  responseStatusCode: 200,
  responseBody: "{\"status\":\"ok\"}",
  responseHeaders: {
    "content-type": "application/json",
  },
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `deliveredAt`                                                                                 | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | N/A                                                                                           | 2024-01-01T00:00:00Z                                                                          |
| `status`                                                                                      | [components.DeliveryAttemptStatus](../../models/components/deliveryattemptstatus.md)          | :heavy_minus_sign:                                                                            | N/A                                                                                           | success                                                                                       |
| `responseStatusCode`                                                                          | *number*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           | 200                                                                                           |
| `responseBody`                                                                                | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           | {"status":"ok"}                                                                               |
| `responseHeaders`                                                                             | Record<string, *string*>                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           | {<br/>"content-type": "application/json"<br/>}                                                |