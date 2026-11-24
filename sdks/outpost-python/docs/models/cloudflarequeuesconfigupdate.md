# CloudflareQueuesConfigUpdate

Partial Cloudflare Queues config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `account_id`                                                       | *Optional[str]*                                                    | :heavy_minus_sign:                                                 | Cloudflare Account ID (32-character hex string).                   |
| `queue_id`                                                         | *Optional[str]*                                                    | :heavy_minus_sign:                                                 | Cloudflare Queue ID (32-character hex string, not the queue name). |