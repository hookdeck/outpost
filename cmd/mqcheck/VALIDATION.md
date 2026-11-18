# Validating a queue change

Runbook for checking a change to a queue consumer or a broker integration (a new broker, a limit, a client upgrade) against [the requirements](REQUIREMENTS.md): first on local brokers, then, for what local brokers can't show, on the real broker.

## 1. Build the change with mqcheck

Work in a checkout of the change. If its branch predates `cmd/mqcheck`, merge or rebase it onto a main that has it, locally.

```sh
go build -o bin/mqcheck ./cmd/mqcheck
bin/mqcheck providers        # is the broker there?
bin/mqcheck cases
```

- The broker has no provider yet: follow [ADDING_A_PROVIDER.md](ADDING_A_PROVIDER.md) first.
- The change adds a byte limit under a name other than `DELIVERY_MAX_CONCURRENCY_BYTES`: set `worker.bytes_env` in a copy of the profile. Byte cases report NOT-IN-BUILD until the setting exists.
- The change alters how the delivery consumer is built: mqcheck builds the delivery queue from Outpost's config and the consumer with `services.NewDeliveryConsumerWorker`, as the delivery service does. A setting wired through either is picked up; one wired elsewhere in the service builder needs the same wiring in `internal/mqcheck/sut/worker.go`.

## 2. Quick tier on local brokers

```sh
make -C cmd/mqcheck up
bin/mqcheck doctor --provider <p>
bin/mqcheck validate --provider <p>          # quick profile, a few minutes
```

Read `report.md` from the top. The verdict word and the exit code tell four outcomes apart: `PASS` (0) only when every case was judged and passed; `FAIL` (1); `ERROR` (2); `NO FAIL, NOT FULLY JUDGED (...)` (3) when nothing failed but some cases couldn't be judged here (NOT-OBSERVABLE, NOT-IN-BUILD, PARTIAL) or capacity was only measured. Exit 3 is not a pass: the unjudged parts need the real broker (step 4) or the right build.

For each FAIL, decide which it is:

- **A finding** in the consumer: report it with the numbers from the check (want / got) and the case's raw files (`cases/<id>/`).
- **Documented broker behavior** that makes the check fail whatever the consumer does: add it to the provider's `Caps.ByDesign` with a link to the broker's docs. Implementation choices are never by design.
- **A harness problem** (wrong expectation, flaky timing): fix the harness, in its own commit.

Never change thresholds or cases to make a change pass.

To see what the change changed, run the same profile on main and compare the requirement tables: a FAIL on both is pre-existing, a FAIL only on the change is a regression (requirement 16: with new settings unset, behavior is unchanged).

## 3. Official tier, locally

```sh
bin/mqcheck validate --provider <p> --profile official      # 30-60 min; run it in the background
```

Every case is judged at production-like limits, plus the capacity envelope: the rate sweep (max sustained rate with queue latency in target), and large-payload drains (throughput, memory). Local brokers bound these numbers: LocalStack's throughput is not SQS's. Treat local capacity as relative (before vs after the change), not absolute.

## 4. Real broker

Run on the real broker what the local setup can't show: everything reported NOT-OBSERVABLE (the Pub/Sub emulator enforces no flow control and doesn't time leases), and capacity numbers that mean something.

Everything is configuration; no account is built in. Use a project or account meant for testing, and a prefix that names you (lowercase letters and digits, no `-`):

```sh
# GCP Pub/Sub
MQCHECK_GCP_EMULATOR_HOST=off MQCHECK_GCP_PROJECT=<test-project> \
MQCHECK_GCP_CREDENTIALS_FILE=<key.json> \
  bin/mqcheck validate --provider gcppubsub --profile official --prefix mqc<you>

# AWS SQS
MQCHECK_SQS_ENDPOINT=aws MQCHECK_SQS_REGION=<region> \
  bin/mqcheck validate --provider awssqs --profile official --prefix mqc<you>
```

- Credentials: Pub/Sub needs to create and delete topics and subscriptions (Pub/Sub Editor on the project); dead-lettering also needs the Pub/Sub service agent to publish to the dead-letter topic and subscribe to the subscription. SQS needs to create, delete, send, receive and read attributes of queues.
- Pub/Sub on the real service uses a 60 s visibility timeout (Outpost's), so cases that wait for timeouts take minutes.
- Latency and capacity are only meaningful when mqcheck runs in the broker's region (a VM or pod there). From elsewhere, read them as relative.
- Every case deletes its queue. After an interrupted run: `bin/mqcheck sweep --provider <p> --prefix mqc<you> --older-than 0s`.
- Real brokers bill per request and per byte; the official profile publishes a few hundred thousand messages.

## 5. Hand back

- The verdict line and the failing requirements with their numbers.
- BY-DESIGN checks with the broker behavior behind each.
- What was NOT-OBSERVABLE locally and whether the real-broker run observed it.
- The capacity table (max sustained rate, latency at the target rate, memory at large payloads) with the settings it is valid for (limits, handler latency, payload, where it ran).
- The build (`run.json`: commit, dirty) and the report paths.
