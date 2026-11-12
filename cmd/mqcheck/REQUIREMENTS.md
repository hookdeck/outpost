# Queue consumer requirements

What every internal queue consumer in Outpost must do, independent of the broker and of how the consumer is built. mqcheck checks these; each case in the report names the requirement it tests.

Queues: delivery, publish, log. Brokers: GCP Pub/Sub, AWS SQS, RabbitMQ, Azure Service Bus, NATS JetStream, in-memory (tests only).

Outpost is built for high throughput: thousands of messages per second and more, scaled out across processes. What one process takes on in a burst must stay bounded and predictable on both burst vectors: message count and message bytes (payloads can be several MB).

## Terms

- **Handled**: the message's business logic is running (the delivery attempt, handling the published event, writing the log entry). A message waiting in a buffer, for a free slot or in a batcher is not handled.
- **Held but not handled**: received from the broker, under its visibility timeout, not settled (acked, nacked, rejected) and not handled: prefetched, buffered in a client library, waiting for a slot.
- **Visibility timeout**: the broker's per-message timer after which an unsettled message is delivered again (Pub/Sub ack deadline, SQS visibility timeout, Service Bus lock duration, JetStream AckWait). RabbitMQ has none.
- **Baseline**: the same consumer with no limits.
- **N**: max messages handled at once, per process and queue. **B**: max message bytes handled at once. Either may be unset.

## Limits

1. **Count.** Never more than N messages handled at once.
2. **Bytes.** Message bytes held by the process (handled, or received and waiting to be handled) stay within B plus one message. If the broker measures size differently (Pub/Sub counts compressed size), the docs say so and how to size B.
3. **Oversized message.** A message larger than B is still processed: taken no later than when nothing else is held, nothing else is taken while it is held, and the queue keeps moving after it.

## Waiting

Path: broker → consumer → handler. The visibility timeout covers handling, nothing before it.

4. **Waiting doesn't count.** Time a message spends before its handler starts (in the broker, or in the consumer because of a limit or anything else) does not run its visibility timeout. A message is never processed twice, redelivered, given an extra delivery attempt or dead-lettered because it waited, however long the wait.
5. **At a limit, the consumer stops taking messages.** It holds at most one message beyond what it handles; the rest stays in the broker, available to other consumers.

## Throughput

6. **Unlimited under the limits.** While neither limit is reached, throughput and latency match the baseline: backlog drain, burst after idle, sustained high rate, slow trickle.
7. **Full use of the limit.** With a backlog, the process runs close to the limit that binds.
8. **Mixed sizes.** Small messages wait behind large ones only while the byte limit is full. No ordering is promised.
9. **Horizontal scale.** More processes on the same queue add throughput in proportion, until the broker or the handlers' dependencies limit it.

## Delivery

10. **At least once.** Every message is processed. Ack removes it; nack makes it available again; a handler that exceeds the visibility timeout gets it redelivered; repeated failures end in the dead-letter queue (delivery, log: the one Outpost provisions; publish: as the operator's broker settings and `PUBLISH_MAX_REDELIVERIES` decide).
11. **The consumer adds no duplicates.** A message is handled more than once only after a failure, a nack, a handler past its timeout, or a shutdown. Holding a message under its visibility timeout without handling it never produces a second copy. Duplicates the broker itself produces (at-least-once delivery) are allowed and reported.

## Lifecycle

12. **Shutdown.** Stops taking messages, lets in-progress handlers finish within the grace period and settles them before exit, and returns everything else to the broker at once (another consumer can take it within seconds) without using a delivery attempt.
13. **Restart and broker outage.** Nothing is lost; consumption resumes on its own.
14. **Idle.** Near-zero CPU and few broker requests while the queue is empty.
15. **Errors.** Permanent problems (missing queue, no permission, at start or while running) surface as errors; they don't spin or fail silently.

## Configuration

16. **Off = today.** With no byte limit set, behavior is unchanged from the previous release.
17. **Same meaning everywhere.** N and B mean the same on every broker; any difference is documented.

## Pass thresholds

Defaults, set in the profiles (`profiles/*.yaml`, `thresholds:`).

| Requirement | Threshold |
|---|---|
| 4 | held but not handled, per message: p99 ≤ 1 s, max ≤ 10 % of the visibility timeout; 0 redeliveries, 0 extra attempts, 0 dead-lettered |
| 5 | ≤ 1 message held beyond those handled; another consumer gets its first message ≤ 2 s after subscribing |
| 6 | drain and burst-after-idle time ≤ 1.2 × baseline (median of 3 runs); sustained rate kept with backlog flat; start delay p95 ≤ max(1.2 × baseline, baseline + 50 ms) |
| 7 | time-weighted mean handled count or bytes ≥ 80 % of the binding limit during the backlog |
| 8 | small-message throughput while bytes aren't full: as 6; after the last large message settles, back to baseline within 1 s |
| 9 | K processes: ≥ 0.8 × K × one process, up to the broker's limit |
| 12 | returned messages received by another consumer within 5 s; process exits within the grace period |
| 13 | after a restart, first message handled ≤ 5 s after the process subscribed |
| 14 | ≤ 10 broker requests per minute per subscription; CPU ≤ 2 millicores above the same process with consumers stopped |
| 15 | the error is logged and the worker fails or exits within 90 s |
| 2, 5 | process memory at a burst within 10 % of the same burst at the limit's equivalent count (memory does not grow with the burst) |

Capacity targets (official profile, one consumer hop, broker in the same region): ≥ 1000 messages/s of ~6 KB JSON with the backlog flat; queue latency (publish → handler start) p50 ≤ 25 ms, p99 ≤ 100 ms; large payloads drained, and with a byte limit B set, peak RSS ≤ idle RSS + 3 × (B + one message).

## What the harness observes

For each requirement, per broker:

- messages and bytes handled over time (the worker's own gauge, exact);
- messages and bytes **held but not handled**: visibly held (returned by the queue client, handler not started; exact, per message wait) and held inside client libraries (the broker's in-flight count minus what the worker handles and visibly holds; only where the broker has a live in-flight count);
- broker backlog over time;
- delivery attempts per message (where the broker exposes them to the consumer), redeliveries, dead-lettered messages; handler invocations per message, each extra one classified: after a nack, after a handler past its timeout, after a shutdown, held past the timeout (consumer-added), or unexplained (reported as a broker duplicate);
- throughput and latency: drain time, sustained rate, queue latency (publish → handler start), end to end (publish → settle);
- process memory (RSS, heap) and CPU;
- time for another consumer to receive returned messages after shutdown.

## Cases

| Case | Req | Scenario |
|---|---|---|
| C1.1 | 1 | backlog of 20 × N small messages, N set, 50 ms handler: never more than N at once |
| C1.3 | 1 | N = 1: never two at once |
| C2.1 | 2 | 64 large messages, B = 16 × message size: handled + held bytes ≤ B + one message |
| C3.1 | 3 | 6 messages larger than B: one at a time, all processed |
| C4.1 | 4, 11 | backlog of 5 × N, handler 60 % of the visibility timeout: messages wait several timeouts for a slot; no wait over the threshold, no redelivery, no duplicate, nothing dead-lettered |
| C4.2 | 4, 11 | as C4.1 with the byte limit binding (large messages, B = 4 × size) |
| C5.1 | 5 | consumer A at its count limit; held beyond the limit ≤ 1; consumer B, started later, gets work within 2 s |
| C5.2 | 5 | as C5.1 with the byte limit binding |
| C10.1 | 10 | all acked: each handled once, broker empty, nothing dead-lettered |
| C10.2 | 10 | 10 % nacked once: those redelivered (attempt + 1), the rest handled once |
| C10.3 | 10 | handler runs 1.5 × the visibility timeout: redelivered, not before the timeout |
| C10.4 | 10 | always nacked: dead-lettered after max attempts |
| C12.1 | 12 | SIGTERM a consumer at its limit, start another: in-progress finish and settle, exit within grace, the rest reaches the other consumer within 5 s without an extra attempt |
| C13.1 | 13 | stop and restart mid-backlog: nothing lost, first message within 5 s |
| C15.1 | 15 | queue missing at start: error logged, worker fails |

Not covered yet: 6, 7, 8, 9, 14, 16, 17, and the publish and log queues (see README, "Not covered yet").
