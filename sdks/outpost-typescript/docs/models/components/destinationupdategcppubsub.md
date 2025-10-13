# DestinationUpdateGCPPubSub

## Example Usage

```typescript
import { DestinationUpdateGCPPubSub } from "@hookdeck/outpost-sdk/models/components";

let value: DestinationUpdateGCPPubSub = {
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

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        | Example                                                                            |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `topics`                                                                           | *components.Topics*                                                                | :heavy_minus_sign:                                                                 | "*" or an array of enabled topics.                                                 | *                                                                                  |
| `config`                                                                           | [components.GCPPubSubConfig](../../models/components/gcppubsubconfig.md)           | :heavy_minus_sign:                                                                 | N/A                                                                                |                                                                                    |
| `credentials`                                                                      | [components.GCPPubSubCredentials](../../models/components/gcppubsubcredentials.md) | :heavy_minus_sign:                                                                 | N/A                                                                                |                                                                                    |