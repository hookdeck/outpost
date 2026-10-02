# OperatorEventRetryRequest

## Example Usage

```typescript
import { OperatorEventRetryRequest } from "@hookdeck/outpost-sdk/models/components";

let value: OperatorEventRetryRequest = {
  eventId: "F1JKYUvykB5grOoE6Bnm6jztlF",
  destinationId: "des_12345",
};
```

## Fields

| Field                                                        | Type                                                         | Required                                                     | Description                                                  | Example                                                      |
| ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `eventId`                                                    | *string*                                                     | :heavy_check_mark:                                           | The ID of the operator event to redeliver.                   | F1JKYUvykB5grOoE6Bnm6jztlF                                   |
| `destinationId`                                              | *string*                                                     | :heavy_check_mark:                                           | The ID of the operator event destination to redeliver it to. | des_12345                                                    |