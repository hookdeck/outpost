# Awss3ConfigUpdate

Partial AWS S3 config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { Awss3ConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: Awss3ConfigUpdate = {};
```

## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `bucket`                                           | *string*                                           | :heavy_minus_sign:                                 | The name of your AWS S3 bucket.                    |
| `region`                                           | *string*                                           | :heavy_minus_sign:                                 | The AWS region where your bucket is located.       |
| `keyTemplate`                                      | *string*                                           | :heavy_minus_sign:                                 | JMESPath expression for generating S3 object keys. |
| `storageClass`                                     | *string*                                           | :heavy_minus_sign:                                 | The storage class for the S3 objects.              |