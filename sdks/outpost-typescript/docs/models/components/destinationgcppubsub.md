# DestinationGCPPubSub

## Example Usage

```typescript
import { DestinationGCPPubSub } from "@hookdeck/outpost-sdk/models/components";

let value: DestinationGCPPubSub = {
  id: "des_gcp_pubsub_123",
  type: "gcp_pubsub",
  topics: [
    "order.created",
    "order.updated",
  ],
  disabledAt: null,
  createdAt: new Date("2024-03-10T14:30:00Z"),
  config: {
    projectId: "my-project-123",
    topic: "events-topic",
  },
  credentials: {
    serviceAccountJson:
      "{\"type\":\"service_account\",\"project_id\":\"my-project-123\",...}",
  },
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | Control plane generated ID or user provided ID for the destination.                           | des_12345                                                                                     |
| `type`                                                                                        | [components.DestinationGCPPubSubType](../../models/components/destinationgcppubsubtype.md)    | :heavy_check_mark:                                                                            | Type of the destination.                                                                      | gcp_pubsub                                                                                    |
| `topics`                                                                                      | *components.Topics*                                                                           | :heavy_check_mark:                                                                            | "*" or an array of enabled topics.                                                            | *                                                                                             |
| `disabledAt`                                                                                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | ISO Date when the destination was disabled, or null if enabled.                               | <nil>                                                                                         |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | ISO Date when the destination was created.                                                    | 2024-01-01T00:00:00Z                                                                          |
| `config`                                                                                      | [components.GCPPubSubConfig](../../models/components/gcppubsubconfig.md)                      | :heavy_check_mark:                                                                            | N/A                                                                                           |                                                                                               |
| `credentials`                                                                                 | [components.GCPPubSubCredentials](../../models/components/gcppubsubcredentials.md)            | :heavy_check_mark:                                                                            | N/A                                                                                           |                                                                                               |
| `target`                                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | A human-readable representation of the destination target (project/topic). Read-only.         | my-project-123/events-topic                                                                   |
| `targetUrl`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | A URL link to the destination target (GCP Console link to the topic). Read-only.              | https://console.cloud.google.com/cloudpubsub/topic/detail/events-topic?project=my-project-123 |