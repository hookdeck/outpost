# KafkaCredentialsUpdate

Partial Kafka credentials for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { KafkaCredentialsUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: KafkaCredentialsUpdate = {};
```

## Fields

| Field                             | Type                              | Required                          | Description                       |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `username`                        | *string*                          | :heavy_minus_sign:                | SASL username for authentication. |
| `password`                        | *string*                          | :heavy_minus_sign:                | SASL password for authentication. |