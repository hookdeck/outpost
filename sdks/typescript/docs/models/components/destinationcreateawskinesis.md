# DestinationCreateAWSKinesis

## Example Usage

```typescript
import { DestinationCreateAWSKinesis } from "openapi/models/components";

let value: DestinationCreateAWSKinesis = {
  id: "user-provided-id",
  type: "aws_kinesis",
  topics: "*",
  config: {
    streamName: "my-data-stream",
    region: "us-east-1",
    endpoint: "https://kinesis.us-east-1.amazonaws.com",
    partitionKeyTemplate: "data.\"user_id\"",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    session: "AQoDYXdzEPT//////////wEXAMPLE...",
  },
};
```

## Fields

| Field                                                                                                    | Type                                                                                                     | Required                                                                                                 | Description                                                                                              | Example                                                                                                  |
| -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `id`                                                                                                     | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | Optional user-provided ID. A UUID will be generated if empty.                                            | user-provided-id                                                                                         |
| `type`                                                                                                   | [components.DestinationCreateAWSKinesisType](../../models/components/destinationcreateawskinesistype.md) | :heavy_check_mark:                                                                                       | Type of the destination. Must be 'aws_kinesis'.                                                          |                                                                                                          |
| `topics`                                                                                                 | *components.Topics*                                                                                      | :heavy_check_mark:                                                                                       | "*" or an array of enabled topics.                                                                       | *                                                                                                        |
| `config`                                                                                                 | [components.AWSKinesisConfig](../../models/components/awskinesisconfig.md)                               | :heavy_check_mark:                                                                                       | N/A                                                                                                      |                                                                                                          |
| `credentials`                                                                                            | [components.AWSKinesisCredentials](../../models/components/awskinesiscredentials.md)                     | :heavy_check_mark:                                                                                       | N/A                                                                                                      |                                                                                                          |