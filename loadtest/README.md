# Load testing

Two suites live here.

## `app/` — Go load test app

A single binary that is both the event publisher and the mock webhook receiver, so every
published event can be reconciled against its delivery. Control-plane REST API, dashboard,
and mock receiver share one port (`:9090`).

Built for multi-tenant, long-running runs against Outpost Cloud: tenants and destinations
are provisioned through the Outpost API, publish rate and mock latency/error rate are
adjustable mid-flight, and per-event status is tracked in a per-group event log.

```bash
make up/loadtest    # Air hot-reload on the shared outpost docker network
```

Dashboard at http://localhost:9090. See [app/](./app/).

## `k6/` — k6 scenario suite

The original TypeScript/k6 suite: scenario + environment config, local (minikube) and AWS
(EKS) infrastructure, and a Grafana dashboard.

```bash
cd loadtest/k6
```

See [k6/docs/](./k6/docs/) and `contributing/loadtest/overview.md`.

## Which to use

`app/` for throughput ceilings, sustained soaks, and anything needing per-event delivery
accounting or mid-run parameter changes. `k6/` for scripted scenario runs on the existing
AWS/minikube infrastructure.
