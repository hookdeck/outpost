# mqcheck: queue consumer validation

mqcheck checks Outpost's internal queue consumer against the [queue requirements](REQUIREMENTS.md) on a real message broker, and measures its capacity envelope. It answers two questions about a queue change:

- **Conformance**: does the consumer meet each requirement on this broker (count and byte limits, waiting, at-least-once, duplicates, shutdown, restart, errors)?
- **Capacity**: what rate does one consumer sustain with latency in target, and what memory do large payloads take, at given limits?

It is a developer tool: it needs brokers and takes minutes, so the harness itself never runs in CI (its unit tests do).

- Validating a queue change: [VALIDATION.md](VALIDATION.md)
- Adding a broker: [ADDING_A_PROVIDER.md](ADDING_A_PROVIDER.md)

## Quick start

```sh
make -C cmd/mqcheck up                                # LocalStack + Pub/Sub emulator, own ports
make -C cmd/mqcheck validate PROVIDER=awssqs          # quick profile, ~1 min
make -C cmd/mqcheck validate PROVIDER=gcppubsub
make -C cmd/mqcheck validate PROVIDER=awssqs PROFILE=official
make -C cmd/mqcheck down
```

Or the binary directly:

```sh
go build -o bin/mqcheck ./cmd/mqcheck
bin/mqcheck providers                       # providers and their settings
bin/mqcheck cases                           # cases and the requirements they test
bin/mqcheck validate --provider awssqs --profile quick [--cases C4.1,C12.1] [--no-capacity]
```

Exit code: 0 PASS (everything judged and passed), 1 FAIL, 2 harness or infrastructure error, 3 no FAIL but not fully judged (some cases not observable on this broker or not in this build, or capacity measured without targets). The verdict word says the same: `PASS`, `FAIL`, `ERROR`, `NO FAIL, NOT FULLY JUDGED (...)`. The last lines on stdout are the verdict and the report path.

## How it works

```
driver (mqcheck validate)                    worker process (mqcheck worker), one per consumer
  provider: provision queue + DLQ  ─────┐      Outpost config from env (as an operator sets it)
  publish realistic messages            │      → delivery queue + consumer worker under the supervisor,
  start / SIGTERM / kill workers  ──────┼────►   built by the service builder's own code
  sample the broker (backlog, in-flight)│      → synthetic handler (decodes the task, runs for the
  read events, judge checks, report     │        profile's time, acks or nacks as the message says)
                                        └────  events on fd 3: ready, start, end, samples, exit
```

- **The consumer under test is production code.** The worker reads Outpost's own environment variables (`DELIVERY_MAX_CONCURRENCY`, broker settings), builds the delivery queue from them as the service does, and builds the consumer worker with `services.NewDeliveryConsumerWorker`, the function the delivery service itself calls. Only the delivery logic is replaced by a synthetic handler. A setting wired through the queue config or that function (a byte limit, say) is picked up without changing mqcheck; one wired anywhere else in the service builder needs the same wiring in `internal/mqcheck/sut/worker.go`.
- **The core knows no broker.** Cases, runner, worker, analysis and report talk to brokers through the `provider.Provider` interface. Each broker is one file in `internal/mqcheck/providers/` that registers itself.
- **Held but not handled** is measured without hooks in the consumer: messages returned by the queue client whose handler hasn't started (exact, per message), and messages held inside client libraries = the broker's in-flight count minus what the worker handles and holds visibly (where the broker has a live in-flight count).
- **Payloads** are valid delivery tasks with webhook-like nested JSON (gzip ratio 2-4) or random bytes, each message different. Never repeated-character padding.

## Reading a report

`out/<time>-<provider>-<profile>/report.md` starts with the verdict, then failing requirements with the numbers, then a requirement table, every case's checks (want / got), the capacity envelope and what the provider can't show. `results.json`, `envelope.json`, `run.json`, `verdict.json` hold the same for tools; `cases/<id>/` has each worker's event stream (`w1.events.jsonl`), log and the broker samples.

Statuses:

| Status | Meaning |
|---|---|
| PASS | every check was judged and passed |
| FAIL | a check failed: a finding |
| BY-DESIGN | a check failed because of documented broker behavior (listed under Provider in the report) |
| PARTIAL | judged checks passed; others can't be observed on this broker |
| NOT-OBSERVABLE | this broker setup can't show it (e.g. the Pub/Sub emulator enforces no flow control). Never a pass |
| MEASURED | capacity numbers recorded but not judged: the broker is an emulation, the profile sets no rate target, or a drain had no byte limit to bound its memory |
| NOT-IN-BUILD | the build lacks the setting the case needs (byte limit cases without `DELIVERY_MAX_CONCURRENCY_BYTES`) |
| ERROR | the harness or the broker failed; the case says nothing about the consumer |

A run limited with `--cases`, `--capacity-only` or `--no-capacity` is never a PASS (exit 3, "subset"). A failing precondition (e.g. the backlog never drained) fails the case; a passing one doesn't make it pass.

## Profiles

`profiles/quick.yaml` (correctness, scaled-down timeouts, cases in parallel, a capacity smoke step that isn't judged) and `profiles/official.yaml` (every case judged, production-like limits, capacity sweep 250 → 8000 messages/s and 1-8 MB drains). Pass `--profile path/to/file.yaml` for your own; every run records the resolved profile in `run.json`.

## Providers

| Provider | Local | Real broker | In-flight count | Notes |
|---|---|---|---|---|
| `awssqs` | LocalStack (`make up`, port 44566) | `MQCHECK_SQS_ENDPOINT=aws`, region and credentials by env | exact (LocalStack), approximate (SQS) | per-message attempts from `ApproximateReceiveCount` |
| `gcppubsub` | emulator (`make up`, port 48085) | `MQCHECK_GCP_EMULATOR_HOST=off`, `MQCHECK_GCP_PROJECT`, credentials file or ADC | none | emulator: flow control, lease timing and capacity not observable |

Resources are named `<prefix>-<run id>-<case>`; every case tears its queue down. `mqcheck sweep --provider P --older-than 2h` deletes what a crashed run left.

## Layout

```
cmd/mqcheck/                 CLI, profiles, docs, compose.yml, Makefile
internal/mqcheck/provider    Provider interface, Caps, registry, Outpost publisher helper
internal/mqcheck/providers   one file per broker (registers itself)
internal/mqcheck/sut         worker process: consumer under test, synthetic handler, event stream
internal/mqcheck/runner      driver: queues, workers, recordings, analysis, capacity
internal/mqcheck/cases       the cases
internal/mqcheck/payload     message generator
internal/mqcheck/profile     profile loading
internal/mqcheck/report      report.md and JSON
```

## Not covered yet

- Requirements 6-9 (throughput relative to a baseline, full use of the limit, mixed sizes, horizontal scale), 14 (idle), 16 (off = previous release), 17 (cross-broker summary), and 13's broker outage (needs a fault-injecting proxy).
- The publish and log queues: the worker runs the delivery queue only.
- Bursts in the capacity envelope; repetitions with medians.
- Providers for RabbitMQ, Azure Service Bus, NATS JetStream and in-memory.
- Running the driver inside a cluster next to the broker. Today mqcheck runs wherever its binary runs; for latency numbers against a cloud broker, run it in the broker's region.
