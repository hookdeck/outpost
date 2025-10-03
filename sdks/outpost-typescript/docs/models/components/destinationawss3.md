# DestinationAwss3

## Example Usage

```typescript
import { DestinationAwss3 } from "@hookdeck/outpost-sdk/models/components";

let value: DestinationAwss3 = {
  id: "des_s3_789",
  type: "aws_s3",
  topics: [
    "*",
  ],
  disabledAt: null,
  createdAt: new Date("2024-03-20T12:00:00Z"),
  config: {
    bucket: "my-bucket",
    region: "us-east-1",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  },
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | Control plane generated ID or user provided ID for the destination.                           | des_12345                                                                                     |
| `type`                                                                                        | [components.DestinationAwss3Type](../../models/components/destinationawss3type.md)            | :heavy_check_mark:                                                                            | Type of the destination.                                                                      | aws_s3                                                                                        |
| `topics`                                                                                      | *components.Topics*                                                                           | :heavy_check_mark:                                                                            | "*" or an array of enabled topics.                                                            | *                                                                                             |
| `disabledAt`                                                                                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | ISO Date when the destination was disabled, or null if enabled.                               | <nil>                                                                                         |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | ISO Date when the destination was created.                                                    | 2024-01-01T00:00:00Z                                                                          |
| `config`                                                                                      | [components.Awss3Config](../../models/components/awss3config.md)                              | :heavy_check_mark:                                                                            | N/A                                                                                           |                                                                                               |
| `credentials`                                                                                 | [components.Awss3Credentials](../../models/components/awss3credentials.md)                    | :heavy_check_mark:                                                                            | N/A                                                                                           |                                                                                               |
| `target`                                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | A human-readable representation of the destination target (bucket and region). Read-only.     | my-bucket in us-east-1                                                                        |
| `targetUrl`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | A URL link to the destination target (AWS Console link to the bucket). Read-only.             | <nil>                                                                                         |