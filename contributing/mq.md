# Message Queues (MQ)

Outpost integrates with message queues in three ways:

1. Internal MQs (deliverymq, logmq)
2. Publish MQs
3. MQ Destination Type

This document will focus on the first two types of integrations. The third type, MQ Destination Type, will be covered in a separate document.

### Internal MQs

- Outpost manages the infrastructure, provisioning it on startup.
- Outpost acts as both the publisher and the subscriber.

### PublishMQ

- Outpost does NOT manage the infrastructure; it must be provisioned by the user outside of Outpost.
- Outpost does NOT act as the publisher.
- Outpost acts as the subscriber.

## Supported MQs

- [x] AWS SQS
- [x] GCP PubSub
- [x] Azure ServiceBus
- [x] RabbitMQ (AMQP 0.9.1)
- [x] NATS JetStream

publishmq only:

- [ ] Kafka

## Configuration

The common configuration (policy) for all MQs includes:

- Visibility Timeout: The period of time during which a message received from the queue is invisible to other consumers (TODO - not configurable yet - Outpost will employ the default visibility timeout for each MQ).
- Retry Limit: The maximum number of times a message can be retried before it is moved to a dead-letter queue.
- Consumer Max Concurrency: The maximum number of consumers that can subscribe to the message queue simultaneously.

In addition to these, each MQ requires its own specific configuration for Outpost to use.

## Attributes

Internal MQs are one of the most impactful aspects of the performance and reliability of Outpost. It's important to understand each individual MQ's characteristics, behavior, and configurable attributes to make an informed decision. This document aims to provide relevant attributes and behavior of these MQs from an Outpost contributor's perspective. From the operator (user) point of view, there are other important characteristics (performance, throughput, pricing, etc.) that are beyond the scope of this document.

The relevant attributes are:

- Visibility Timeout: The period of time during which a message received from the queue is invisible to other consumers.
- Retry Limit: The maximum number of times a message can be retried before it is moved to a dead-letter queue.
- Retry Behavior: How the MQ behaves when a message is not successfully processed.
- DLQ Name & Behavior: The name and behavior of the dead-letter queue.

These attributes can be further elaborated in a later part of this document for each supported MQ.

Regarding the dead-letter queue (DLQ), it is up to the operator to observe the DLQ and act on any arrived messages. Outpost provisions the queue only and does not subscribe to or act further on it and its messages.

## Implementation checklist

- [ ] implement mq interface at `internal/mqs`
- [ ] implement infrastructure interface at `internal/mqinfra`
- [ ] mock support for `internal/destinationmockserver`
- [ ] documentation
  - [ ] `.env.example`
- [ ] e2e test suite (TODO)
- [ ] benchmark & load test (TODO)

## MQ Behavior & Attributes

### AWS SQS

**Configuration**:

- Queue name (required)
- Credentials (required)
- Region (optional - default to us-east-1)
- Endpoint (optional - default to AWS's endpoint)

**Infrastructure**:

When using AWS SQS for internal mq, Outpost will provision these infrastructure:

1. queue
2. dead-letter queue: queue name + `-dlq`

**Visibility Timeout**:

The default value is 30 seconds. The maximum value is 12 hours.

**Retry Behavior**:

**When a message is nacked, it stays invisible until the visibility timeout expires.** After reaching the retry limit, the message will be sent to the DLQ.

### RabbitMQ

**Configuration**:

- Server URL (required)
- Exchange name (required)
- Queue name (required)

**Infrastructure**:

When using RabbitMQ for internal mq, Outpost will provision these infrastructure:

1. exchange
2. queue
4. dead-lettered queue: queue name + `.dlq`

Specifically, Outpost will provision one main topic exchange (default name `outpost`) and use routing key to route messages to each queues. For each queue, there is a dead-lettered queue with the same name + `.dlq` bounded to the main exchange.

**Visibility Timeout**:

RabbitMQ's concept of visibility timeout is called [acknowledgement timeout](https://www.rabbitmq.com/docs/consumers#acknowledgement-timeout). The default value is 30min, and the minimum value is 1min, although the official documentation recomends against value below 5min.

**Retry Behavior**:

**When a message is nacked, it is retried immediately.** After reaching the retry limit, the message will be sent to the DLX (dead-lettered exchange) and it then routed to the DLQ.

### NATS JetStream

**Configuration**:

- Server URL (required)
- Stream name (required)
- Subject (required) — one per queue type (deliverymq/logmq); both share one stream
- DLQ subject (optional) — defaults to `dlq.<subject>` if unset

**Infrastructure**:

When using NATS JetStream for internal mq, Outpost will provision this infrastructure:

1. main stream, scoped to `<stream>.>` so deliverymq and logmq can each declare it without one wiping out the other's subject
2. a separate DLQ stream, subject `dlq.<subject>` by default
3. durable consumer on the main stream, filtered to its own subject

The DLQ subject intentionally lives outside the main stream's subject namespace (`dlq.` prefix rather than `<subject>.dlq`) — JetStream doesn't allow two streams to claim overlapping subjects, and nesting under the main stream's wildcard would do exactly that.

**Visibility Timeout**:

JetStream's equivalent is `AckWait` — the time a delivered-but-unacked message stays invisible before redelivery. Outpost uses a fixed 60s.

**Retry Behavior**:

**When a message is nacked, it's redelivered after `AckWait` elapses.** Unlike RabbitMQ/SQS, JetStream has no broker-native DLQ — once a message's delivery count exceeds the configured retry limit, Outpost explicitly republishes it to the DLQ subject and terminates the original.
