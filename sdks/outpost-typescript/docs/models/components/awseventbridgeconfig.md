# AWSEventBridgeConfig

## Example Usage

```typescript
import { AWSEventBridgeConfig } from "@hookdeck/outpost-sdk/models/components";

let value: AWSEventBridgeConfig = {
  eventBusName: "my-event-bus",
  region: "us-east-1",
  endpoint: "https://events.us-east-1.amazonaws.com",
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          | Example                                                                                              |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `eventBusName`                                                                                       | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Optional. The name or ARN of the EventBridge event bus. Defaults to the account's default event bus. | my-event-bus                                                                                         |
| `region`                                                                                             | *string*                                                                                             | :heavy_check_mark:                                                                                   | The AWS region where the event bus is located.                                                       | us-east-1                                                                                            |
| `endpoint`                                                                                           | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Optional. Custom AWS endpoint URL (e.g., for LocalStack or VPC endpoints).                           | https://events.us-east-1.amazonaws.com                                                               |