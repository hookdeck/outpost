# GCPPubSubConfigUpdate

Partial GCP Pub/Sub config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { GCPPubSubConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: GCPPubSubConfigUpdate = {};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `projectId`                                                        | *string*                                                           | :heavy_minus_sign:                                                 | The GCP project ID.                                                |
| `topic`                                                            | *string*                                                           | :heavy_minus_sign:                                                 | The Pub/Sub topic name.                                            |
| `endpoint`                                                         | *string*                                                           | :heavy_minus_sign:                                                 | Optional. Custom endpoint URL (e.g., localhost:8085 for emulator). |