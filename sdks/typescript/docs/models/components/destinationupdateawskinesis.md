# DestinationUpdateAWSKinesis

## Example Usage

```typescript
import { DestinationUpdateAWSKinesis } from "openapi/models/components";

let value: DestinationUpdateAWSKinesis = {
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

| Field                                                                                | Type                                                                                 | Required                                                                             | Description                                                                          | Example                                                                              |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `topics`                                                                             | *components.Topics*                                                                  | :heavy_minus_sign:                                                                   | "*" or an array of enabled topics.                                                   | *                                                                                    |
| `config`                                                                             | [components.AWSKinesisConfig](../../models/components/awskinesisconfig.md)           | :heavy_minus_sign:                                                                   | N/A                                                                                  |                                                                                      |
| `credentials`                                                                        | [components.AWSKinesisCredentials](../../models/components/awskinesiscredentials.md) | :heavy_minus_sign:                                                                   | N/A                                                                                  |                                                                                      |