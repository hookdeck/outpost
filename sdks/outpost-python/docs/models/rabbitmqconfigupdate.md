# RabbitMQConfigUpdate

Partial RabbitMQ config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `server_url`                                                                     | *Optional[str]*                                                                  | :heavy_minus_sign:                                                               | RabbitMQ server address (host:port).                                             |
| `exchange`                                                                       | *Optional[str]*                                                                  | :heavy_minus_sign:                                                               | The exchange to publish messages to.                                             |
| `tls`                                                                            | [Optional[models.RabbitMQConfigUpdateTLS]](../models/rabbitmqconfigupdatetls.md) | :heavy_minus_sign:                                                               | Whether to use TLS connection (amqps).                                           |