# Awss3Config

## Example Usage

```typescript
import { Awss3Config } from "@hookdeck/outpost-sdk/models/components";

let value: Awss3Config = {
  bucket: "my-bucket",
  region: "us-east-1",
  keyTemplate:
    "join('/', [time.year, time.month, time.day, metadata.`\"event-id\"`, '.json'])",
  storageClass: "STANDARD",
};
```

## Fields

| Field                                                                                                                           | Type                                                                                                                            | Required                                                                                                                        | Description                                                                                                                     | Example                                                                                                                         |
| ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `bucket`                                                                                                                        | *string*                                                                                                                        | :heavy_check_mark:                                                                                                              | The name of your AWS S3 bucket.                                                                                                 | my-bucket                                                                                                                       |
| `region`                                                                                                                        | *string*                                                                                                                        | :heavy_check_mark:                                                                                                              | The AWS region where your bucket is located.                                                                                    | us-east-1                                                                                                                       |
| `keyTemplate`                                                                                                                   | *string*                                                                                                                        | :heavy_minus_sign:                                                                                                              | JMESPath expression for generating S3 object keys. Default is join('', [time.rfc3339_nano, '_', metadata."event-id", '.json']). | join('/', [time.year, time.month, time.day, metadata.`"event-id"`, '.json'])                                                    |
| `storageClass`                                                                                                                  | *string*                                                                                                                        | :heavy_minus_sign:                                                                                                              | The storage class for the S3 objects (e.g., STANDARD, INTELLIGENT_TIERING, GLACIER, etc.). Defaults to "STANDARD".              | STANDARD                                                                                                                        |