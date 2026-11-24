# RabbitMQCredentialsUpdate

Partial RabbitMQ credentials for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { RabbitMQCredentialsUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: RabbitMQCredentialsUpdate = {};
```

## Fields

| Field              | Type               | Required           | Description        |
| ------------------ | ------------------ | ------------------ | ------------------ |
| `username`         | *string*           | :heavy_minus_sign: | RabbitMQ username. |
| `password`         | *string*           | :heavy_minus_sign: | RabbitMQ password. |