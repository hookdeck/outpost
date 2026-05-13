# KafkaConfig

## Example Usage

```typescript
import { KafkaConfig } from "@hookdeck/outpost-sdk/models/components";

let value: KafkaConfig = {
  brokers: "broker1.example.com:9092,broker2.example.com:9092",
  topic: "events",
  saslMechanism: "scram-sha-256",
  partitionKeyTemplate: "data.customer_id",
};
```

## Fields

| Field                                                                                                     | Type                                                                                                      | Required                                                                                                  | Description                                                                                               | Example                                                                                                   |
| --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `brokers`                                                                                                 | *string*                                                                                                  | :heavy_check_mark:                                                                                        | Comma-separated list of Kafka broker addresses.                                                           | broker1.example.com:9092,broker2.example.com:9092                                                         |
| `topic`                                                                                                   | *string*                                                                                                  | :heavy_check_mark:                                                                                        | The Kafka topic to publish messages to.                                                                   | events                                                                                                    |
| `saslMechanism`                                                                                           | [components.SaslMechanism](../../models/components/saslmechanism.md)                                      | :heavy_check_mark:                                                                                        | SASL authentication mechanism.                                                                            | scram-sha-256                                                                                             |
| `tls`                                                                                                     | [components.KafkaConfigTls](../../models/components/kafkaconfigtls.md)                                    | :heavy_minus_sign:                                                                                        | Whether to enable TLS for the connection.                                                                 | true                                                                                                      |
| `partitionKeyTemplate`                                                                                    | *string*                                                                                                  | :heavy_minus_sign:                                                                                        | Optional JMESPath template to extract the partition key from the event payload. Defaults to the event ID. | data.customer_id                                                                                          |