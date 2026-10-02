# UpdateOperatorEventDestinationRequest

## Example Usage

```typescript
import { UpdateOperatorEventDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: UpdateOperatorEventDestinationRequest = {
  destinationId: "<id>",
  body: {
    topics: [
      "alert.destination.disabled",
      "alert.attempt.exhausted_retries",
    ],
    config: {
      "url": "https://alerts.acme.com/outpost",
    },
    credentials: {
      "rotate_secret": true,
    },
  },
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `destinationId`                                                                                        | *string*                                                                                               | :heavy_check_mark:                                                                                     | The ID of the operator event destination.                                                              |
| `body`                                                                                                 | [components.OperatorEventDestinationUpdate](../../models/components/operatoreventdestinationupdate.md) | :heavy_check_mark:                                                                                     | N/A                                                                                                    |