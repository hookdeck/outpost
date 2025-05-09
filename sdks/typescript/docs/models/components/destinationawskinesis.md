# DestinationAWSKinesis

## Example Usage

```typescript
import { DestinationAWSKinesis } from "openapi/models/components";

let value: DestinationAWSKinesis = {
  id: "des_kns_xyz",
  type: "aws_kinesis",
  topics: [
    "user.created",
    "user.updated",
  ],
  disabledAt: null,
  createdAt: new Date("2024-03-10T15:30:00Z"),
  config: {
    streamName: "production-events",
    region: "eu-west-1",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  },
};
```

## Fields

| Field                                                                                                             | Type                                                                                                              | Required                                                                                                          | Description                                                                                                       | Example                                                                                                           |
| ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `id`                                                                                                              | *string*                                                                                                          | :heavy_check_mark:                                                                                                | Control plane generated ID or user provided ID for the destination.                                               | des_12345                                                                                                         |
| `type`                                                                                                            | [components.DestinationAWSKinesisType](../../models/components/destinationawskinesistype.md)                      | :heavy_check_mark:                                                                                                | Type of the destination.                                                                                          | aws_kinesis                                                                                                       |
| `topics`                                                                                                          | *components.Topics*                                                                                               | :heavy_check_mark:                                                                                                | "*" or an array of enabled topics.                                                                                | *                                                                                                                 |
| `disabledAt`                                                                                                      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                     | :heavy_check_mark:                                                                                                | ISO Date when the destination was disabled, or null if enabled.                                                   | <nil>                                                                                                             |
| `createdAt`                                                                                                       | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                     | :heavy_check_mark:                                                                                                | ISO Date when the destination was created.                                                                        | 2024-01-01T00:00:00Z                                                                                              |
| `config`                                                                                                          | [components.AWSKinesisConfig](../../models/components/awskinesisconfig.md)                                        | :heavy_check_mark:                                                                                                | N/A                                                                                                               |                                                                                                                   |
| `credentials`                                                                                                     | [components.AWSKinesisCredentials](../../models/components/awskinesiscredentials.md)                              | :heavy_check_mark:                                                                                                | N/A                                                                                                               |                                                                                                                   |
| `target`                                                                                                          | *string*                                                                                                          | :heavy_minus_sign:                                                                                                | A human-readable representation of the destination target (Kinesis stream name). Read-only.                       | production-events                                                                                                 |
| `targetUrl`                                                                                                       | *string*                                                                                                          | :heavy_minus_sign:                                                                                                | A URL link to the destination target (AWS Console link to the stream). Read-only.                                 | https://eu-west-1.console.aws.amazon.com/kinesis/home?region=eu-west-1#/streams/details/production-events/details |