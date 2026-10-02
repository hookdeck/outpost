# CloudflareQueuesCredentialsUpdate

Partial Cloudflare Queues credentials for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { CloudflareQueuesCredentialsUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: CloudflareQueuesCredentialsUpdate = {};
```

## Fields

| Field                                                                                                          | Type                                                                                                           | Required                                                                                                       | Description                                                                                                    |
| -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `apiToken`                                                                                                     | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | Cloudflare API Token with the Account > Queues > Edit (Queues Write) permission, scoped to the target account. |