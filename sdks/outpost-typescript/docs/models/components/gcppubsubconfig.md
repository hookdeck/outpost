# GCPPubSubConfig

## Example Usage

```typescript
import { GCPPubSubConfig } from "@hookdeck/outpost-sdk/models/components";

let value: GCPPubSubConfig = {
  projectId: "my-project-123",
  topic: "events-topic",
  endpoint: "pubsub.googleapis.com:443",
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        | Example                                                            |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `projectId`                                                        | *string*                                                           | :heavy_check_mark:                                                 | The GCP project ID.                                                | my-project-123                                                     |
| `topic`                                                            | *string*                                                           | :heavy_check_mark:                                                 | The Pub/Sub topic name.                                            | events-topic                                                       |
| `endpoint`                                                         | *string*                                                           | :heavy_minus_sign:                                                 | Optional. Custom endpoint URL (e.g., localhost:8085 for emulator). | pubsub.googleapis.com:443                                          |