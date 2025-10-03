# UpdateTenantDestinationResponse

Destination updated successfully or OAuth redirect needed.


## Supported Types

### `components.Destination`

```typescript
const value: components.Destination = {
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

