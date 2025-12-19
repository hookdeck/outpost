# Outpost

## Quick Start

```sh
# Clone and setup
git clone https://github.com/hookdeck/outpost.git
cd outpost
cp .env.dev .env
cp .outpost.yaml.dev .outpost.yaml

# Create Docker network (one-time setup)
make network

# Start everything
make up/deps
make up/outpost
```

The API is now available at `http://localhost:3333`.

**Default setup includes:**
- Redis (cache and state)
- PostgreSQL (log store)
- RabbitMQ (message queue)

See [Configuration](#configuration) to customize with ClickHouse, AWS SQS, GCP PubSub, Azure ServiceBus, and more.

## Day-to-Day Development

```sh
# Start
make up/deps
make up/outpost

# Stop
make down/outpost
make down/deps

# Or use shortcuts
make up    # starts deps + outpost
make down  # stops outpost + deps
```

## Running Tests

```sh
# Start test infrastructure
make up/test

# Run tests
make test TESTINFRA=1

# Run specific package
make test TESTINFRA=1 TEST=./internal/config

# Run specific test
make test TESTINFRA=1 TEST=./internal/config RUN=TestValidateService

# With verbose output
make test TESTINFRA=1 TEST=./internal/config RUN=TestValidateService TESTARGS="-v"

# Stop test infrastructure
make down/test
```

See the [Test](test.md) documentation for more details.

## Configuration

Outpost can be configured via environment variables (`.env`) or a YAML file (`.outpost.yaml`). See [Configuration](config.md) for all options.

> **Tip:** For smoother local DX, consider using `.outpost.yaml` for Outpost settings. Air detects changes to `.outpost.yaml` and automatically restarts the services, whereas `.env` changes require `make down/outpost && make up/outpost`.

### Local Dependencies (.env)

The `.env` file controls which services `make up/deps` starts. Add or remove `LOCAL_DEV_*` variables to customize your stack:

```sh
# .env

# Cache (Redis is default, uncomment to use Dragonfly instead)
# LOCAL_DEV_DRAGONFLY=1

# Log store
LOCAL_DEV_POSTGRES=1

# Message queue
LOCAL_DEV_RABBITMQ=1

# Optional: Additional services
# LOCAL_DEV_CLICKHOUSE=1
# LOCAL_DEV_LOCALSTACK=1
# LOCAL_DEV_GCP=1

# Optional: GUI tools (set port to enable)
# LOCAL_DEV_REDIS_COMMANDER=28081
# LOCAL_DEV_PGADMIN=28082
# LOCAL_DEV_TABIX=28083
```

| Variable | Description |
|----------|-------------|
| `LOCAL_DEV_DRAGONFLY=1` | Use Dragonfly instead of Redis |
| `LOCAL_DEV_POSTGRES=1` | Enable PostgreSQL |
| `LOCAL_DEV_CLICKHOUSE=1` | Enable ClickHouse |
| `LOCAL_DEV_RABBITMQ=1` | Enable RabbitMQ |
| `LOCAL_DEV_LOCALSTACK=1` | Enable LocalStack (AWS SQS) |
| `LOCAL_DEV_GCP=1` | Enable GCP PubSub emulator |
| `LOCAL_DEV_REDIS_COMMANDER=<port>` | Enable Redis Commander |
| `LOCAL_DEV_PGADMIN=<port>` | Enable pgAdmin |
| `LOCAL_DEV_TABIX=<port>` | Enable Tabix (ClickHouse UI) |

After changing `.env`, restart dependencies:

```sh
make down/deps
make up/deps
```

> **Note:** Azure ServiceBus has its own stack (`make up/azure`) because it's shared between dev and test environments.

### Host Ports Reference

| Service | Host Port | Credentials |
|---------|-----------|-------------|
| Redis/Dragonfly | localhost:26379 | password: password |
| PostgreSQL | localhost:25432 | outpost:outpost |
| ClickHouse TCP | localhost:29000 | outpost:outpost |
| ClickHouse HTTP | localhost:28123 | outpost:outpost |
| RabbitMQ AMQP | localhost:25672 | guest:guest |
| RabbitMQ UI | localhost:25673 | guest:guest |
| LocalStack (AWS) | localhost:24566 | test:test |
| GCP PubSub | localhost:28085 | - |

## Viewing Logs

```sh
SERVICE=api make logs
SERVICE=delivery make logs
SERVICE=log make logs

# With options
SERVICE=api ARGS="--tail 50" make logs
```

## Development Options

### Option 1: Docker (Recommended)

The Docker setup includes live reload via Air, so code changes are automatically reflected.

### Option 2: Manual

Run Outpost services directly:

```sh
# Start all services
go run cmd/outpost/main.go

# Or specific services
go run cmd/outpost/main.go --service api
go run cmd/outpost/main.go --service delivery
go run cmd/outpost/main.go --service log
```

Note: You'll need to configure `.outpost.yaml` or `.env` with correct host addresses (e.g., `localhost` instead of `redis`).

## Optional Tools

### OpenTelemetry

OTEL is disabled by default. To enable, see the [Uptrace setup](https://github.com/hookdeck/outpost/tree/main/build/dev/uptrace).

### Kubernetes

For local Kubernetes deployment, see the [Kubernetes guide](https://github.com/hookdeck/outpost/tree/main/deployments/kubernetes).
