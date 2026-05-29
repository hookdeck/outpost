# WebhookConfigUpdate

Partial Webhook config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { WebhookConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: WebhookConfigUpdate = {
  url: "https://example.com/webhooks/user",
  customHeaders:
    "{\"x-api-key\":\"secret123\",\"x-tenant-id\":\"customer-456\"}",
};
```

## Fields

| Field                                                                     | Type                                                                      | Required                                                                  | Description                                                               | Example                                                                   |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `url`                                                                     | *string*                                                                  | :heavy_minus_sign:                                                        | The URL to send the webhook events to.                                    | https://example.com/webhooks/user                                         |
| `customHeaders`                                                           | *string*                                                                  | :heavy_minus_sign:                                                        | JSON string of custom HTTP headers to include with every webhook request. | {"x-api-key":"secret123","x-tenant-id":"customer-456"}                    |