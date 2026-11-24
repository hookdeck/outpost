# RabbitMQConfigUpdate

Partial RabbitMQ config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                                                     | Type                                                                                      | Required                                                                                  | Description                                                                               |
| ----------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `ServerURL`                                                                               | `*string`                                                                                 | :heavy_minus_sign:                                                                        | RabbitMQ server address (host:port).                                                      |
| `Exchange`                                                                                | `*string`                                                                                 | :heavy_minus_sign:                                                                        | The exchange to publish messages to.                                                      |
| `TLS`                                                                                     | [*components.RabbitMQConfigUpdateTLS](../../models/components/rabbitmqconfigupdatetls.md) | :heavy_minus_sign:                                                                        | Whether to use TLS connection (amqps).                                                    |