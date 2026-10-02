# CreateTenantDestinationRequest

## Example Usage

```typescript
import { CreateTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: CreateTenantDestinationRequest = {
  tenantId: "<id>",
  body: {
    type: "cloudflare_queues",
    topics: "*",
    config: {
      accountId: "023e105f4ecef8ad9ca31a8372d0c353",
      queueId: "9d7d4cf8a3a14d9aaeb50c3e74e2f4b1",
    },
    credentials: {
      apiToken: "v1.0-1234567890abcdef...",
    },
  },
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `body`                                                                | *components.DestinationCreate*                                        | :heavy_check_mark:                                                    | N/A                                                                   |