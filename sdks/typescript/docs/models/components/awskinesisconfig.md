# AWSKinesisConfig

## Example Usage

```typescript
import { AWSKinesisConfig } from "openapi/models/components";

let value: AWSKinesisConfig = {
  streamName: "my-data-stream",
  region: "us-east-1",
  endpoint: "https://kinesis.us-east-1.amazonaws.com",
  partitionKeyTemplate: "data.\"user_id\"",
};
```

## Fields

| Field                                                                                                                                | Type                                                                                                                                 | Required                                                                                                                             | Description                                                                                                                          | Example                                                                                                                              |
| ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ |
| `streamName`                                                                                                                         | *string*                                                                                                                             | :heavy_check_mark:                                                                                                                   | The name of the AWS Kinesis stream.                                                                                                  | my-data-stream                                                                                                                       |
| `region`                                                                                                                             | *string*                                                                                                                             | :heavy_check_mark:                                                                                                                   | The AWS region where the Kinesis stream is located.                                                                                  | us-east-1                                                                                                                            |
| `endpoint`                                                                                                                           | *string*                                                                                                                             | :heavy_minus_sign:                                                                                                                   | Optional. Custom AWS endpoint URL (e.g., for LocalStack or VPC endpoints).                                                           | https://kinesis.us-east-1.amazonaws.com                                                                                              |
| `partitionKeyTemplate`                                                                                                               | *string*                                                                                                                             | :heavy_minus_sign:                                                                                                                   | Optional. JMESPath template to extract the partition key from the event payload (e.g., `metadata."event-id"`). Defaults to event ID. | data."user_id"                                                                                                                       |