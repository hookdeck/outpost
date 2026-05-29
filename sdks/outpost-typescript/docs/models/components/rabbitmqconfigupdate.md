# RabbitMQConfigUpdate

Partial RabbitMQ config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { RabbitMQConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: RabbitMQConfigUpdate = {};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `serverUrl`                                                                              | *string*                                                                                 | :heavy_minus_sign:                                                                       | RabbitMQ server address (host:port).                                                     |
| `exchange`                                                                               | *string*                                                                                 | :heavy_minus_sign:                                                                       | The exchange to publish messages to.                                                     |
| `tls`                                                                                    | [components.RabbitMQConfigUpdateTls](../../models/components/rabbitmqconfigupdatetls.md) | :heavy_minus_sign:                                                                       | Whether to use TLS connection (amqps).                                                   |