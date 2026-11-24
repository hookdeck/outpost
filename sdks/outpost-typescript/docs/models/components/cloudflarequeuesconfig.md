# CloudflareQueuesConfig

## Example Usage

```typescript
import { CloudflareQueuesConfig } from "@hookdeck/outpost-sdk/models/components";

let value: CloudflareQueuesConfig = {
  accountId: "023e105f4ecef8ad9ca31a8372d0c353",
  queueId: "9d7d4cf8a3a14d9aaeb50c3e74e2f4b1",
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        | Example                                                            |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `accountId`                                                        | *string*                                                           | :heavy_check_mark:                                                 | Cloudflare Account ID (32-character hex string).                   | 023e105f4ecef8ad9ca31a8372d0c353                                   |
| `queueId`                                                          | *string*                                                           | :heavy_check_mark:                                                 | Cloudflare Queue ID (32-character hex string, not the queue name). | 9d7d4cf8a3a14d9aaeb50c3e74e2f4b1                                   |