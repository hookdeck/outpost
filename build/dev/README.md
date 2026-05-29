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
applied. There are no `up/<addon>` targets — flipping the flag is the
mechanism.

## Layout

| dir | purpose | gating |
|---|---|---|
| `compose.yml` | core (api, delivery, log, portal) — always on | — |
| `deps/` | redis/dragonfly, postgres, clickhouse, rabbitmq, localstack, gcp + GUIs | compose profiles, per service |
| `envoy/` | forward proxy for `DESTINATIONS_WEBHOOK_PROXY_URL` flows | `LOCAL_DEV_ENVOY=1` |
| `grafana/` | otel-collector + Prometheus + Grafana | `LOCAL_DEV_GRAFANA=1` |
| `uptrace/` | otel-collector + Uptrace (alternative to grafana) | `LOCAL_DEV_UPTRACE=1` |
| `azure/` | Azure Service Bus + SQL Edge emulators | `LOCAL_DEV_AZURE=1` |

Add-ons are file-level: the file is only loaded when its flag is set. This
avoids cross-file service-name collisions (e.g. `otel-collector` exists in
both `grafana/` and `uptrace/`).

## Adding a new add-on

1. Create `build/dev/<name>/compose.yml` with `name: "outpost"` at the top
   and no `networks:` block (compose creates the default network).
2. Add a `LOCAL_DEV_<NAME>=1` branch in `dev.sh` appending the file.
3. Add the flag to `.env.dev` (commented) and document it in
   `contributing/getting-started.md`.

## Commands

```
make up      # bring up everything enabled in .env
make down    # stop and remove the stack
make nuke    # stop + remove volumes (wipe state)
make up/portal   # run portal natively for vite hot reload (escape hatch)
make up/test     # separate test project (isolated lifecycle)
```
