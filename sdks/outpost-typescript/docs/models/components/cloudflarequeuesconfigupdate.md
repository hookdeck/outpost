# CloudflareQueuesConfigUpdate

Partial Cloudflare Queues config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { CloudflareQueuesConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: CloudflareQueuesConfigUpdate = {};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `accountId`                                                        | *string*                                                           | :heavy_minus_sign:                                                 | Cloudflare Account ID (32-character hex string).                   |
| `queueId`                                                          | *string*                                                           | :heavy_minus_sign:                                                 | Cloudflare Queue ID (32-character hex string, not the queue name). |