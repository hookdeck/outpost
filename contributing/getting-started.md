# Outpost

## Quick Start

```sh
# Clone and setup
git clone https://github.com/hookdeck/outpost.git
cd outpost
cp .env.dev .env
cp .outpost.yaml.dev .outpost.yaml

# Start everything
make up
```

The API is now available at `http://localhost:3333`.

**Default setup includes:**
- Redis (cache and state)
- ClickHouse (log store)
- RabbitMQ (message queue)

See [Configuration](#configuration) to customize with PostgreSQL, AWS SQS, GCP PubSub, Azure ServiceBus, and more.

## Day-to-Day Development

```sh
make up      # bring up everything enabled in .env
make down    # stop and remove the stack
make nuke    # stop + remove volumes (wipe state)
```

`make up` is declarative — it reads `.env` and reconciles the running stack
to match. Edit `.env`, re-run `make up`, and only the diff is applied.

### Verifying the stack

```sh
make health              # reachability check, polls up to 5s by default
make health WAIT=30      # override poll window (handy right after make up)
make smoke               # end-to-end pipeline test (tenant → publish → log)
```

`make health` walks the same `LOCAL_DEV_*` flags as `make up` and probes
only services that should be running. `make smoke` provisions a tenant +
webhook destination, publishes one event to `https://mock.hookdeck.com`,
and polls until the delivery attempt lands in the log store, then cleans
up — exercising api, redis, mq, delivery worker, egress, log worker, and
log store in one shot.

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

> **Tip:** For smoother local DX, consider using `.outpost.yaml` for Outpost settings. Air detects changes to `.outpost.yaml` and automatically restarts the services, whereas `.env` changes require `make down && make up`.

### Local Dev Stack (.env)

The `.env` file controls which services `make up` starts. Add or remove `LOCAL_DEV_*` variables to customize your stack, then re-run `make up`:

```sh
# .env

# Cache (Redis is default, uncomment to use Dragonfly instead)
# LOCAL_DEV_DRAGONFLY=1

# Log store
LOCAL_DEV_CLICKHOUSE=1

# Message queue
LOCAL_DEV_RABBITMQ=1

# Optional: Additional services
# LOCAL_DEV_POSTGRES=1
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
| `LOCAL_DEV_ENVOY=1` | Forward proxy for `DESTINATIONS_WEBHOOK_PROXY_URL` testing |
| `LOCAL_DEV_GRAFANA=1` | OTel collector + Prometheus + Grafana |
| `LOCAL_DEV_UPTRACE=1` | Uptrace observability (alternative to Grafana — cannot be combined) |
| `LOCAL_DEV_AZURE=1` | Azure Service Bus + SQL Edge emulators |

After changing `.env`, reconcile the stack:

```sh
make up
```

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
