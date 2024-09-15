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

Keep in mind you can't use `-run "Test..."` along with `make test/integration` as the integration test already specify integration tests with `-run` option. However, since you're already specifying which test to run, I assume this is a non-issue.

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
