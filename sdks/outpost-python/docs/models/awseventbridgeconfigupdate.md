# AWSEventBridgeConfigUpdate

Partial AWS EventBridge config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `event_bus_name`                                                                                     | *Optional[str]*                                                                                      | :heavy_minus_sign:                                                                                   | Optional. The name or ARN of the EventBridge event bus. Defaults to the account's default event bus. |
| `region`                                                                                             | *Optional[str]*                                                                                      | :heavy_minus_sign:                                                                                   | The AWS region where the event bus is located.                                                       |
| `endpoint`                                                                                           | *Optional[str]*                                                                                      | :heavy_minus_sign:                                                                                   | Optional. Custom AWS endpoint URL (e.g., for LocalStack or VPC endpoints).                           |