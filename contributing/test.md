# Test

## Test Runner

The test suite uses `scripts/test.sh` as the unified test runner. It provides consistent behavior across different commands with support for [gotestsum](https://github.com/gotestyourself/gotestsum) (recommended) or plain `go test`.

### Runner Behavior

By default, the script auto-detects the available runner:

1. If `RUNNER` env var is set, use that explicitly
2. If `gotestsum` is installed, use it (with automatic retries for flaky tests)
3. Otherwise, fall back to `go test`

To install gotestsum (recommended):

```sh
go install gotest.tools/gotestsum@latest
```

To force a specific runner:

```sh
RUNNER=go ./scripts/test.sh test    # Force go test
RUNNER=gotestsum ./scripts/test.sh test  # Force gotestsum
```

## Commands

### Using the Test Script

```sh
# Run all tests (unit + integration)
./scripts/test.sh test

# Run unit tests only (uses -short flag)
./scripts/test.sh unit

# Run end-to-end tests
./scripts/test.sh e2e

# Run everything, with every optional test group enabled
./scripts/test.sh full
```

### Using Make

The Makefile targets delegate to `scripts/test.sh`:

```sh
make test        # ./scripts/test.sh test
make test/unit   # ./scripts/test.sh unit
make test/e2e    # ./scripts/test.sh e2e
make test/full   # ./scripts/test.sh full
```

### Using go test Directly

```sh
go test ./...           # All tests
go test ./... -short    # Unit tests only
go test ./... -run "Integration"  # Integration tests
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `TEST` | Package(s) to test | `./internal/...` |
| `RUN` | Filter tests by name pattern | (none) |
| `TESTARGS` | Additional arguments to pass to test command | (none) |
| `TESTINFRA` | Set to `1` to use persistent test infrastructure | (none) |
| `TESTCOMPAT` | Set to `1` to run the alternative backend tests, and the destination tests (see [Test groups](#test-groups)) | (none) |
| `TESTDEST` | Set to `1` to run the destination provider tests (see [Test groups](#test-groups)) | (none) |
| `TESTAZURE` | Set to `1` to run the Azure Service Bus tests (see [Test groups](#test-groups)) | (none) |
| `RUNNER` | Force test runner: `gotestsum` or `go` | auto-detect |

### Examples

```sh
# Test specific package
TEST='./internal/services/api' make test

# Run specific tests
RUN='TestJWT' make test

# Pass additional options
TESTARGS='-v' make test

# Combine options
RUN='TestListTenant' TEST='./internal/models' TESTINFRA=1 make test
```

## Coverage

1. Run test coverage

```sh
make test/coverage

# or with go test directly
go test ./... -coverprofile=coverage.out

# or to test specific package
TEST='./internal/services/api' make test/coverage
```

2. Visualize test coverage

Running the coverage test command above will generate the `coverage.out` file. You can visually inspect the test coverage with this command to see which statements are covered and more.

```sh
$ make test/coverage/html
# go tool cover -html=coverage.out
```

## Test groups

Integration tests (`TestIntegration…`, and the e2e suites) need external services. They form a ladder: by default `make test` runs what a default dev setup uses, so a test run needs few services and stays fast. `TESTDEST ⊂ TESTCOMPAT`: each row of the table adds to the rows above it. `TESTAZURE` is separate and adds only the Azure tests. A test outside the enabled groups skips with a message naming its flag.

| Flag | Adds | Services |
|------|------|----------|
| (none) | Log stores (ClickHouse, Postgres), Redis and Dragonfly, NATS JetStream as the internal queue, e2e suites on NATS + ClickHouse + Dragonfly | test stack: `make up/test` (ClickHouse, Postgres, NATS, mock server). Redis and Dragonfly always run as testcontainers |
| `TESTDEST=1` | Destination providers, every case: RabbitMQ, AWS SQS, Kinesis, S3 and EventBridge, GCP Pub/Sub, Kafka, and the e2e test that delivers to S3 | + destination stack: `make up/dest` (RabbitMQ, LocalStack, Pub/Sub emulator, Kafka) |
| `TESTCOMPAT=1` | Outpost on the other internal queues: RabbitMQ, AWS SQS and GCP Pub/Sub (`internal/mqs`, `internal/mqinfra`, the RabbitMQ variants of the worker restart tests in `internal/services`, e2e on RabbitMQ), and the backend compat suites (e2e on Postgres, Redis Stack, and Redis Cluster when `TEST_REDIS_CLUSTER_URL` is set, see `make up/test/rediscluster`). Includes `TESTDEST` | + destination stack (same brokers) |
| `TESTAZURE=1` (separate) | Azure Service Bus as internal queue and destination; doesn't enable the rows above | Azure Service Bus emulator (`LOCAL_DEV_AZURE=1` in `.env`, then `make up`) |

`./scripts/test.sh full` (`make test/full`) sets `TESTCOMPAT=1` and `TESTDEST=1`. `-short` (`make test/unit`, and CI) skips every integration test whatever the flags say.

```sh
# Run the Kafka destination tests
export TESTINFRA=1 TESTDEST=1
make up/test      # with TESTDEST=1 it also runs make up/dest
TEST='./internal/destregistry/providers/destkafka' make test

# Release check: everything except Azure
export TESTINFRA=1 TESTCOMPAT=1
make up/test
make test/full
```

### Destination stack

`make up/dest` starts the brokers Outpost delivers to, as compose project `outpost-dest` (`build/dest/compose.yml`). One stack serves the `TESTDEST`/`TESTCOMPAT` tests, manual testing against `make up` and the consumers in `cmd/destinations`. `DEST` picks brokers (default all):

```sh
make up/dest                    # rabbitmq aws gcp kafka
make up/dest DEST="kafka aws"   # adds to what runs
make down/dest
```

| Broker | From the host | From `make up` services |
|--------|---------------|-------------------------|
| RabbitMQ | `amqp://guest:guest@localhost:15672` (AMQP, not the management UI), UI `localhost:15673` | `amqp://guest:guest@dest-rabbitmq:5672` |
| LocalStack (SQS, SNS, Kinesis, EventBridge, S3) | `http://localhost:14566`, credentials `test`/`test` | `http://dest-aws:4566` |
| Pub/Sub emulator | `localhost:18085`, any project | `dest-gcp:8085` |
| Kafka plaintext | `localhost:19092` | `dest-kafka:29092` |
| Kafka SASL PLAIN (`admin`/`admin-secret`) | `localhost:19093` | `dest-kafka:29094` |

The `make up` services (api, delivery, log) join the stack's network (`outpost-dest`), so a destination created in your local Outpost uses the right-hand column. The `cmd/destinations` consumers default to the left-hand column; environment variables point them elsewhere (see each `main.go`).

## Integration & E2E Tests

Integration and e2e tests require external services like ClickHouse, Postgres, NATS, etc. The test suite supports two modes for running these:

**Persistent infrastructure (recommended)**: Run `make up/test` once, then use `TESTINFRA=1` for all test runs. This is the recommended approach for local development.

**Testcontainers (fallback)**: Without `TESTINFRA=1`, tests automatically spawn containers via [Testcontainers](https://testcontainers.com/). This is convenient for CI or one-off runs but adds startup overhead.

### Why persistent infrastructure?

Redis and Dragonfly always use testcontainers (one container per test) since they start quickly. Heavier dependencies like LocalStack (AWS) or GCP emulators can take 15-30 seconds to initialize. With persistent infrastructure, you pay this cost once and get fast iteration from then on.

To run the test infrastructure:

```sh
$ make up/test

## to take the test infra down
# $ make down/test
```

It will run a Docker compose stack called `outpost-test` which runs the necessary services at ports ":30000 + port". For example, ClickHouse usually runs on port `:9000`, so in the test infra it will run on port `:39000`.

`make up/test` starts the services the default test run needs: ClickHouse, Postgres, NATS and the mock server. With `TESTDEST=1` or `TESTCOMPAT=1` set, it also starts the [destination stack](#destination-stack). `make down/test` removes the test stack; `make down/dest` the destination stack.

From here, you can provide env variable `TESTINFRA=1` to tell the test suite to use these services instead of spawning testcontainers.

```sh
$ TESTINFRA=1 make test
```

Tip: You can `$ export TESTINFRA=1` (and any group flags) to use the test infra for the whole terminal session.

With `TESTINFRA=1`, a test whose service isn't running fails after about 15 seconds of refused connections and names the command that starts it. Without `TESTINFRA=1`, a test that can't start its testcontainer fails with the Docker error.

### Integration Test Template

Here's a short template for how you can write integration tests that require an external test infra:

```golang
// Integration test should always start with "TestIntegration...() {}"
func TestIntegrationMyIntegrationTest(t *testing.T) {
  t.Parallel()

  // call testinfra.Start(t) to signal that you require the test infra.
  // This helps the test runner properly terminate resources at the end.
  t.Cleanup(testinfra.Start(t))

  // use whichever infra you need
  chConfig := testinfra.NewClickHouseConfig(t)
  natsConfig := testinfra.NewMQNATSConfig(t)
  // ...
}
```

A test that needs a service outside the default set belongs to a [test group](#test-groups): call the group's skip helper first, so the test skips when the flag isn't set.

```golang
func TestIntegrationMyDestinationTest(t *testing.T) {
  testutil.SkipUnlessDest(t) // or testutil.SkipUnlessCompat(t)
  t.Parallel()
  t.Cleanup(testinfra.Start(t))

  awsMQConfig := testinfra.NewMQAWSConfig(t, attributesMap)
  // ...
}
```
