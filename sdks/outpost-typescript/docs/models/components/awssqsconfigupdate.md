# AWSSQSConfigUpdate

Partial AWS SQS config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { AWSSQSConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: AWSSQSConfigUpdate = {
  endpoint: "http://localhost:4566",
  queueUrl: "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
};
```

## Fields

| Field                                                                                                                                                        | Type                                                                                                                                                         | Required                                                                                                                                                     | Description                                                                                                                                                  | Example                                                                                                                                                      |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `endpoint`                                                                                                                                                   | *string*                                                                                                                                                     | :heavy_minus_sign:                                                                                                                                           | Optional. Custom AWS endpoint URL for emulators such as LocalStack. Only accepted when the Outpost deployment sets DESTINATIONS_ALLOW_CUSTOM_ENDPOINTS=true. | http://localhost:4566                                                                                                                                        |
| `queueUrl`                                                                                                                                                   | *string*                                                                                                                                                     | :heavy_minus_sign:                                                                                                                                           | The URL of the SQS queue. A URL that is not an AWS SQS endpoint is only accepted when the Outpost deployment sets DESTINATIONS_ALLOW_CUSTOM_ENDPOINTS=true.  | https://sqs.us-east-1.amazonaws.com/123456789012/my-queue                                                                                                    |