# AWSKinesisConfigUpdate

Partial AWS Kinesis config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { AWSKinesisConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: AWSKinesisConfigUpdate = {};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `streamName`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | The name of the AWS Kinesis stream.                                              |
| `region`                                                                         | *string*                                                                         | :heavy_minus_sign:                                                               | The AWS region where the Kinesis stream is located.                              |
| `endpoint`                                                                       | *string*                                                                         | :heavy_minus_sign:                                                               | Optional. Custom AWS endpoint URL (e.g., for LocalStack or VPC endpoints).       |
| `partitionKeyTemplate`                                                           | *string*                                                                         | :heavy_minus_sign:                                                               | Optional. JMESPath template to extract the partition key from the event payload. |