# AWSEventBridgeConfigUpdate

Partial AWS EventBridge config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { AWSEventBridgeConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: AWSEventBridgeConfigUpdate = {};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `eventBusName`                                                                                       | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Optional. The name or ARN of the EventBridge event bus. Defaults to the account's default event bus. |
| `region`                                                                                             | *string*                                                                                             | :heavy_minus_sign:                                                                                   | The AWS region where the event bus is located.                                                       |
| `endpoint`                                                                                           | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Optional. Custom AWS endpoint URL (e.g., for LocalStack or VPC endpoints).                           |