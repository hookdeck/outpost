# Test

## Commands

1. Run all tests

```sh
$ make test
# go test $(go list ./...)
```

2. Run unit tests

```sh
$ make test/unit
# go test $(go list ./...) -short
```

3. Run integration tests

```sh
$ make test/unit
# go test $(go list ./...) -run "Integration"
```

## Options

1. To test specific package

```sh
$ TEST='./internal/services/api' make test
# go test ./internal/services/api
```

2. To run specific tests or use other options

```sh
$ TESTARGS='-v -run "TestJWT"'' make test
# go test $(go list ./...) -v -run "TestJWT"
```

Keep in mind you can't use `-run "Test..."` along with `make test/integration` as the integration test already specify integration tests with `-run` option. However, since you're already specifying which test to run, we assume this is a non-issue.

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
