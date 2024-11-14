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
# Create the required `outpost` Docker network used by different Docker stacks
$ make network

# start development
$ make up

# stop
$ make down
```

The Docker environment is configured with live reload along with all the infra dependencies (Redis) you need, so you can start coding right away.

_Note_: For more in-depth guide on setting up with Docker, here's a [step by step guide](step-by-step.md) walking you through setting up your environment all the way to you first delivered event.

**Option 2**: Manual

As Outpost has dependency on Redis, please make sure you have a running instance ready. The `.env.example` is set up for Docker usage. When running Outpost services directly, please make sure your `.env` is configured correctly.

To start Outpost services:

```sh
# Start all services
$ go run cmd/eventkit/main.go

# You can specify which service you want to run
$ go run cmd/eventkit/main.go --service api
$ go run cmd/eventkit/main.go --service delivery
$ go run cmd/eventkit/main.go --service log
```

For live reload, you can use [`air`](https://github.com/air-verse/air) or a similar tool. We are currently using Air within the Docker Compose setup.

### Logs

When running Outpost with Docker, you may want to tail the log in your terminal for local development. There's a command to help tailing the log of Outpost's containers.

```sh
$ SERVICE=api make logs
# docker logs $(docker ps -f name=outpost-api --format "{{.ID}}") -f
$ SERVICE=delivery make logs
# docker logs $(docker ps -f name=outpost-delivery --format "{{.ID}}") -f
$ SERVICE=log make logs
# docker logs $(docker ps -f name=outpost-log --format "{{.ID}}") -f
```

You can also add more options to this command like so:

```sh
$ SERVICE=api ARGS="--tail 50" make logs
# docker logs $(docker ps -f name=outpost-api --format "{{.ID}}") -f --tail 50
```

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

Currently, Outpost OpenTelemetry configuration is handled via environemnt variables. You can you any OpenTelemetry backend you'd like. OTEL is off by default. Feel free to customize your configuration accordingly.

There's a sample [Uptrace](https://uptrace.dev/) Docker set up you can use in [this repository](https://github.com/hookdeck/outpost/tree/main/build/dev/uptrace). Please follow the instruction there if you'd like to use Uptrace for your OTEL backend.

**(Optional) Kubernetes**

If you want to deploy Outpost to your local Kubernetes (for some reason), there's a [guide](https://github.com/hookdeck/outpost/tree/main/deployments/kubernetes) for that too. Enjoy!
