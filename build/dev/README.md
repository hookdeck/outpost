# Outpost Dev Stack

One Docker Compose project (`outpost`). One command: `make up`. What runs is
declared in `.env` via `LOCAL_DEV_*` flags.

## How it works

`build/dev/dev.sh` reads `.env`, builds the docker compose invocation —
a list of `-f` files plus `COMPOSE_PROFILES` — and runs it. All files declare
`name: outpost` and join one auto-created network (`outpost_default`), so
service-to-service DNS (`api`, `redis`, `postgres`, …) just works.

```
.env             →   dev.sh   →   docker compose -f ... -f ... --profile ... up -d
LOCAL_DEV_X=1
```

`make up` is declarative: edit `.env`, re-run `make up`, only the diff is
applied, and services whose flag was turned off are removed. Add-ons have no
`up/<addon>` targets — flipping the flag is the mechanism. (`make up/dest`
is a separate stack, not an add-on; see below.)

`dev.sh` also stops `make up` early when the internal queue Outpost will
select (same order as Outpost: SQS, Azure Service Bus, Pub/Sub, RabbitMQ,
NATS) is this stack's RabbitMQ or NATS but its `LOCAL_DEV_*` flag is off, or
when the selected publish queue (SQS, Azure Service Bus, Pub/Sub, RabbitMQ) is
this stack's RabbitMQ without `LOCAL_DEV_RABBITMQ=1`.
Leftover blocks of queues Outpost doesn't select are ignored.

## Layout

| dir | purpose | gating |
|---|---|---|
| `compose.yml` | core (api, delivery, log, portal) — always on | — |
| `deps/` | redis/dragonfly, postgres, clickhouse, nats, rabbitmq, localstack, gcp + GUIs | compose profiles, per service |
| `envoy/` | forward proxy for `DESTINATIONS_PROXY_URL` flows | `LOCAL_DEV_ENVOY=1` |
| `envoy-chain/` | second proxy behind `envoy/`, for hop-list flows | `LOCAL_DEV_ENVOY_CHAIN=1` |
| `grafana/` | otel-collector + Prometheus + Grafana | `LOCAL_DEV_GRAFANA=1` |
| `uptrace/` | otel-collector + Uptrace (alternative to grafana) | `LOCAL_DEV_UPTRACE=1` |
| `azure/` | Azure Service Bus + SQL Edge emulators | `LOCAL_DEV_AZURE=1` |

Add-ons are file-level: the file is only loaded when its flag is set. This
avoids cross-file service-name collisions (e.g. `otel-collector` exists in
both `grafana/` and `uptrace/`).

## Adding a new add-on

1. Create `build/dev/<name>/compose.yml` with `name: "outpost"` at the top
   and no `networks:` block (compose creates the default network). Relative
   paths resolve from `build/dev/` (the directory of the first compose file),
   not from the add-on's directory: mount `./<name>/config.yml`.
2. Add a `LOCAL_DEV_<NAME>=1` branch in `dev.sh` appending the file.
3. Add the flag to `.env.dev` (commented) and document it in
   `contributing/getting-started.md`.

## Commands

```
make up      # bring up everything enabled in .env
make down    # stop and remove the stack
make nuke    # stop + remove volumes of every dev service, including disabled ones (wipe state)
make up/portal   # run portal natively for vite hot reload (escape hatch)
make up/test     # separate test project (isolated lifecycle)
make up/dest     # destination brokers (project outpost-dest, build/dest/)
```

api, delivery and log also join the `outpost-dest` network (created by
`dev.sh` and `make up/dest`), so destinations can point at `dest-rabbitmq`,
`dest-aws`, `dest-gcp`, `dest-kafka`.
