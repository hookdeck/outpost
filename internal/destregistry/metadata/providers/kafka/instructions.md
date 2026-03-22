# Apache Kafka Configuration Instructions

Apache Kafka is a distributed event streaming platform used for building real-time data pipelines and streaming applications. It provides:

- High-throughput, low-latency message delivery
- Durable message storage with configurable retention
- Horizontal scalability through partitioning
- Built-in support for SASL authentication and TLS encryption

## How to configure Kafka as an event destination

To configure Kafka as a destination you must provide:

- **Brokers** — A comma-separated list of Kafka broker addresses (e.g., `broker1:9092,broker2:9092`)
- **Topic** — The Kafka topic to publish messages to

### Optional settings

- **SASL Mechanism** — Authentication mechanism: `plain`, `scram-sha-256`, or `scram-sha-512`. If set, you must also provide **Username** and **Password**.
- **TLS** — Enable TLS encryption for the connection.
- **Partition Key Template** — A JMESPath expression to extract the message key from the event payload. If not set, the event ID is used as the message key.
