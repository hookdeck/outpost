# KafkaCredentials

## Example Usage

```typescript
import { KafkaCredentials } from "@hookdeck/outpost-sdk/models/components";

let value: KafkaCredentials = {
  username: "outpost",
  password: "secure_password_123",
};
```

## Fields

| Field                             | Type                              | Required                          | Description                       | Example                           |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `username`                        | *string*                          | :heavy_check_mark:                | SASL username for authentication. | outpost                           |
| `password`                        | *string*                          | :heavy_check_mark:                | SASL password for authentication. | secure_password_123               |