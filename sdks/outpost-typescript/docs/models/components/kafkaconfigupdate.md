# KafkaConfigUpdate

Partial Kafka config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { KafkaConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: KafkaConfigUpdate = {};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `brokers`                                                                                              | *string*                                                                                               | :heavy_minus_sign:                                                                                     | Comma-separated list of Kafka broker addresses.                                                        |
| `topic`                                                                                                | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The Kafka topic to publish messages to.                                                                |
| `saslMechanism`                                                                                        | [components.KafkaConfigUpdateSaslMechanism](../../models/components/kafkaconfigupdatesaslmechanism.md) | :heavy_minus_sign:                                                                                     | SASL authentication mechanism.                                                                         |
| `tls`                                                                                                  | [components.KafkaConfigUpdateTls](../../models/components/kafkaconfigupdatetls.md)                     | :heavy_minus_sign:                                                                                     | Whether to enable TLS for the connection.                                                              |
| `partitionKeyTemplate`                                                                                 | *string*                                                                                               | :heavy_minus_sign:                                                                                     | Optional JMESPath template to extract the partition key from the event payload.                        |