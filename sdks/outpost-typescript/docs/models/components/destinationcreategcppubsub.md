# DestinationCreateGCPPubSub

## Example Usage

```typescript
import { DestinationCreateGCPPubSub } from "@hookdeck/outpost-sdk/models/components";

let value: DestinationCreateGCPPubSub = {
  id: "user-provided-id",
  type: "gcp_pubsub",
  topics: "*",
  config: {
    projectId: "my-project-123",
    topic: "events-topic",
    endpoint: "pubsub.googleapis.com:443",
  },
  credentials: {
    serviceAccountJson:
      "{\"type\":\"service_account\",\"project_id\":\"my-project\",\"private_key_id\":\"key123\",\"private_key\":\"-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n\",\"client_email\":\"my-service@my-project.iam.gserviceaccount.com\"}",
  },
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            | Example                                                                                                |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `id`                                                                                                   | *string*                                                                                               | :heavy_minus_sign:                                                                                     | Optional user-provided ID. A UUID will be generated if empty.                                          | user-provided-id                                                                                       |
| `type`                                                                                                 | [components.DestinationCreateGCPPubSubType](../../models/components/destinationcreategcppubsubtype.md) | :heavy_check_mark:                                                                                     | Type of the destination. Must be 'gcp_pubsub'.                                                         |                                                                                                        |
| `topics`                                                                                               | *components.Topics*                                                                                    | :heavy_check_mark:                                                                                     | "*" or an array of enabled topics.                                                                     | *                                                                                                      |
| `config`                                                                                               | [components.GCPPubSubConfig](../../models/components/gcppubsubconfig.md)                               | :heavy_check_mark:                                                                                     | N/A                                                                                                    |                                                                                                        |
| `credentials`                                                                                          | [components.GCPPubSubCredentials](../../models/components/gcppubsubcredentials.md)                     | :heavy_check_mark:                                                                                     | N/A                                                                                                    |                                                                                                        |