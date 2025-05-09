# DestinationWebhook

## Example Usage

```typescript
import { DestinationWebhook } from "openapi/models/components";

let value: DestinationWebhook = {
  id: "des_webhook_123",
  type: "webhook",
  topics: [
    "user.created",
    "order.shipped",
  ],
  disabledAt: null,
  createdAt: new Date("2024-02-15T10:00:00Z"),
  config: {
    url: "https://my-service.com/webhook/handler",
  },
  credentials: {
    secret: "whsec_abc123def456",
    previousSecret: "whsec_prev789xyz012",
    previousSecretInvalidAt: new Date("2024-02-16T10:00:00Z"),
  },
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | Control plane generated ID or user provided ID for the destination.                           | des_12345                                                                                     |
| `type`                                                                                        | [components.DestinationWebhookType](../../models/components/destinationwebhooktype.md)        | :heavy_check_mark:                                                                            | Type of the destination.                                                                      | webhook                                                                                       |
| `topics`                                                                                      | *components.Topics*                                                                           | :heavy_check_mark:                                                                            | "*" or an array of enabled topics.                                                            | *                                                                                             |
| `disabledAt`                                                                                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | ISO Date when the destination was disabled, or null if enabled.                               | <nil>                                                                                         |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | ISO Date when the destination was created.                                                    | 2024-01-01T00:00:00Z                                                                          |
| `config`                                                                                      | [components.WebhookConfig](../../models/components/webhookconfig.md)                          | :heavy_check_mark:                                                                            | N/A                                                                                           |                                                                                               |
| `credentials`                                                                                 | [components.WebhookCredentials](../../models/components/webhookcredentials.md)                | :heavy_check_mark:                                                                            | N/A                                                                                           |                                                                                               |
| `target`                                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | A human-readable representation of the destination target (e.g., URL host). Read-only.        | my-service.com                                                                                |
| `targetUrl`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | A URL link to the destination target (the webhook URL). Read-only.                            | https://my-service.com/webhook/handler                                                        |