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

# Run full suite with all backend combinations
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
| `TESTCOMPAT` | Set to `1` to run full backend compatibility suite | (none) |
| `TESTREDISCLUSTER` | Set to `1` to enable Redis cluster tests | (none) |
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
$ make test/coverage
# go test $(go list ./...)  -coverprofile=coverage.out

# or to test specific package
$ TEST='./internal/services/api' make test/coverage
# go test $(go list ./...)  -coverprofile=coverage.out
```

2. Visualize test coverage

Running the coverage test command above will generate the `coverage.out` file. You can visually inspect the test coverage with this command to see which statements are covered and more.

```sh
$ make test/coverage/html
# go tool cover -html=coverage.out
```

## Compatibility Testing

By default, the test suite runs only the primary backends (Miniredis + Dragonfly + Postgres) to keep feedback loops fast. To run the full suite including compatibility tests for alternative backends (Redis Stack, Redis Cluster), set the `TESTCOMPAT` environment variable:

```sh
# Run full test suite including all backend combinations
TESTCOMPAT=1 make test
```

This is useful for release testing or when making changes that might affect Redis compatibility.

## Integration & E2E Tests

When running integration & e2e tests, we often times require some test infrastructure such as ClickHouse, LocalStack, RabbitMQ, etc. We use [Testcontainers](https://testcontainers.com/) for that. It usually takes a few seconds (10s or so) to spawn the necessary containers. To improve the feedback loop, you can run a persistent test infrastructure and skip spawning testcontainers.

To run the test infrastructure:

```sh
$ make up/test

## to take the test infra down
# $ make down/test
```

It will run a Docker compose stack called `outpost-test` which runs the necessary services at ports ":30000 + port". For example, ClickHouse usually runs on port `:9000`, so in the test infra it will run on port `:39000`.

From here, you can provide env variable `TESTINFRA=1` to tell the test suite to use these services instead of spawning testcontainers.

```sh
$ TESTINFRA=1 make test
```

Tip: You can `$ export TESTINFRA=1` to use the test infra for the whole terminal session.

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
  awsMQConfig := testinfra.NewMQAWSConfig(t, attributesMap)
  rabbitmqConfig := testinfra.NewMQRabbitMQConfig(t)
  // ...
}
```
