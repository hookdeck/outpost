# AWSKinesisCredentialsUpdate

Partial AWS Kinesis credentials for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { AWSKinesisCredentialsUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: AWSKinesisCredentialsUpdate = {};
```

## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `key`                                                   | *string*                                                | :heavy_minus_sign:                                      | AWS Access Key ID.                                      |
| `secret`                                                | *string*                                                | :heavy_minus_sign:                                      | AWS Secret Access Key.                                  |
| `session`                                               | *string*                                                | :heavy_minus_sign:                                      | Optional AWS Session Token (for temporary credentials). |