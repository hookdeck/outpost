# RabbitMQConfig


## Fields

| Field                                                       | Type                                                        | Required                                                    | Description                                                 | Example                                                     |
| ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- |
| `ServerURL`                                                 | `string`                                                    | :heavy_check_mark:                                          | RabbitMQ server address (host:port).                        | localhost:5672                                              |
| `Exchange`                                                  | `string`                                                    | :heavy_check_mark:                                          | The exchange to publish messages to.                        | my-exchange                                                 |
| `TLS`                                                       | [*components.TLS](../../models/components/tls.md)           | :heavy_minus_sign:                                          | Whether to use TLS connection (amqps). Defaults to "false". | false                                                       |