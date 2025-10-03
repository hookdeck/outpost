# DestinationCreateAwss3

## Example Usage

```typescript
import { DestinationCreateAwss3 } from "@hookdeck/outpost-sdk/models/components";

let value: DestinationCreateAwss3 = {
  id: "user-provided-id",
  type: "aws_s3",
  topics: "*",
  config: {
    bucket: "my-bucket",
    region: "us-east-1",
    keyTemplate:
      "join('/', [time.year, time.month, time.day, metadata.`\"event-id\"`, '.json'])",
    storageClass: "STANDARD",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    session: "AQoDYXdzEPT//////////wEXAMPLE...",
  },
};
```

## Fields

| Field                                                                                          | Type                                                                                           | Required                                                                                       | Description                                                                                    | Example                                                                                        |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `id`                                                                                           | *string*                                                                                       | :heavy_minus_sign:                                                                             | Optional user-provided ID. A UUID will be generated if empty.                                  | user-provided-id                                                                               |
| `type`                                                                                         | [components.DestinationCreateAwss3Type](../../models/components/destinationcreateawss3type.md) | :heavy_check_mark:                                                                             | Type of the destination. Must be 'aws_s3'.                                                     |                                                                                                |
| `topics`                                                                                       | *components.Topics*                                                                            | :heavy_check_mark:                                                                             | "*" or an array of enabled topics.                                                             | *                                                                                              |
| `config`                                                                                       | [components.Awss3Config](../../models/components/awss3config.md)                               | :heavy_check_mark:                                                                             | N/A                                                                                            |                                                                                                |
| `credentials`                                                                                  | [components.Awss3Credentials](../../models/components/awss3credentials.md)                     | :heavy_check_mark:                                                                             | N/A                                                                                            |                                                                                                |