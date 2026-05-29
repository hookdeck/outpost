# AWSSQSConfigUpdate

Partial AWS SQS config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { AWSSQSConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: AWSSQSConfigUpdate = {
  endpoint: "https://sqs.us-east-1.amazonaws.com",
  queueUrl: "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
};
```

## Fields

| Field                                                                         | Type                                                                          | Required                                                                      | Description                                                                   | Example                                                                       |
| ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `endpoint`                                                                    | *string*                                                                      | :heavy_minus_sign:                                                            | Optional. Custom AWS endpoint URL (e.g., for LocalStack or specific regions). | https://sqs.us-east-1.amazonaws.com                                           |
| `queueUrl`                                                                    | *string*                                                                      | :heavy_minus_sign:                                                            | The URL of the SQS queue.                                                     | https://sqs.us-east-1.amazonaws.com/123456789012/my-queue                     |