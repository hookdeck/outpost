# EventKit

## Development

### 0. Environment

Copy `.env.example` to `.env` to use the default environment variables.

```sh
$ cp .env.example .env
```

### 1. Start services

**Option 1**: Using Docker

You can use Docker to manage your development environment. Here are the 2 `make` commands you can use:

```sh
# start development
$ make up

# stop
$ make down
```

The Docker environment is configured with live reload along with all the infra dependencies (Redis) you need, so you can start coding right away.

**Option 2**: Manual

As EventKit has dependency on Redis, please make sure you have a running instance ready. By default, EventKit will connect with Redis at localhost:6379. You can customize the Redis address in the `.env` file.

To start EventKit services:

```sh
# Start all services
$ go run cmd/eventkit/main.go

# You can specify which service you want to run
$ go run cmd/eventkit/main.go --service api
$ go run cmd/eventkit/main.go --service delivery
$ go run cmd/eventkit/main.go --service log
```

To set up live reload, you can use [`air`](https://github.com/air-verse/air) or a similar tool. We can consider setting the project up with it as well.

### Tests

Some basic commands:

```sh
# Run all tests
$ make test

# Run unit / integration tests
$ make test/unit
$ make test/integration

# Run test coverage
$ make test/coverage
# then visualize it
$ make test/coverage/html

# Run specific test suite / package
$ TEST=./internal/model make test
$ TEST=./internal/model TESTARGS='-v -run "TestDestinationModel"' make test
```

See the [Test](test.md) documentation for further information.

### Others

**(Optional) OpenTelemetry**

Currently, EventKit OpenTelemetry configuration is handled via environemnt variables. You can you any OpenTelemetry backend you'd like. OTEL is off by default. Feel free to customize your configuration accordingly.

There's a sample [Uptrace](https://uptrace.dev/) Docker set up you can use in [this repository](https://github.com/hookdeck/EventKit/tree/main/build/dev/uptrace). Please follow the instruction there if you'd like to use Uptrace for your OTEL backend.

**(Optional) Kubernetes**

If you want to deploy EventKit to your local Kubernetes (for some reason), there's a [guide](https://github.com/hookdeck/EventKit/tree/main/deployments/kubernetes) for that too. Enjoy!
