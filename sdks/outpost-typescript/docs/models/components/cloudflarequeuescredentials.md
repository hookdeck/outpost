# CloudflareQueuesCredentials

## Example Usage

```typescript
import { CloudflareQueuesCredentials } from "@hookdeck/outpost-sdk/models/components";

let value: CloudflareQueuesCredentials = {
  apiToken: "v1.0-1234567890abcdef...",
};
```

## Fields

| Field                                                                                                          | Type                                                                                                           | Required                                                                                                       | Description                                                                                                    | Example                                                                                                        |
| -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `apiToken`                                                                                                     | *string*                                                                                                       | :heavy_check_mark:                                                                                             | Cloudflare API Token with the Account > Queues > Edit (Queues Write) permission, scoped to the target account. | v1.0-1234567890abcdef...                                                                                       |