# GCPPubSubCredentialsUpdate

Partial GCP Pub/Sub credentials for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { GCPPubSubCredentialsUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: GCPPubSubCredentialsUpdate = {};
```

## Fields

| Field                                                                   | Type                                                                    | Required                                                                | Description                                                             |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `serviceAccountJson`                                                    | *string*                                                                | :heavy_minus_sign:                                                      | Service account key JSON. The entire JSON key file content as a string. |