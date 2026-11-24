# WebhookConfigUpdate

Partial Webhook config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                                     | Type                                                                      | Required                                                                  | Description                                                               | Example                                                                   |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `URL`                                                                     | `*string`                                                                 | :heavy_minus_sign:                                                        | The URL to send the webhook events to.                                    | https://example.com/webhooks/user                                         |
| `CustomHeaders`                                                           | `*string`                                                                 | :heavy_minus_sign:                                                        | JSON string of custom HTTP headers to include with every webhook request. | {"x-api-key":"secret123","x-tenant-id":"customer-456"}                    |