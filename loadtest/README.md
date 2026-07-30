# Load testing

Two suites live here, plus the benchmark pipeline that turns a run into figures.

## Running a benchmark

```bash
cp .outpost.yaml.dev .outpost.yaml   # gitignored, needed by the stack
make up/bench                        # deps + outpost + loadtest + prometheus + grafana
make bench                           # runs app/runs/example.yaml
make bench SPEC=path/to/your-spec.yaml
```

`make bench` validates the spec against its concurrency budget, provisions the profiles,
runs warm-up → steady → drain with all profiles crossing together, exports an exact
ledger, folds in the Prometheus series, and renders the figures to `report/charts/`.

**Exit code 3 means the run is void** — the ledger did not balance, or a precondition
failed. A void run is a harness bug, never a result; its figures carry a `VOID RUN`
notice and must not be published.

Grafana is for watching a run in progress. It is never a step in producing the artifact,
because an artifact you have to click for is not reproducible. Prometheus is on host
`:9091` — the app owns `:9090`.

### Specs

A spec is the complete description of a run, and the only input the pipeline needs.
`app/runs/example.yaml` is the one in this repo: a four-minute pipeline check that
documents the format field by field.

Real specs are **not** kept here. `bench.sh` reads a spec locally and POSTs it, so
neither the deployment nor the repo needs a copy — and the run export embeds the spec
that produced it verbatim, which is the reproduction record that matters.

Two rules when sizing one:

- `concurrency_budget` is the **target deployment's `delivery_max_concurrency`**, the
  worker pool deliveries queue for. Exceeding it does not make the number slightly
  worse, it changes the measurement: deliveries queue, events age past the sweep window,
  and the run reports a backlog where a latency should be.
- Set `budget_tolerance` around `0.8`. A queueing system near full utilisation has
  unbounded wait, so jitter alone starts a queue that never drains.

Concurrency is Little's law over *deliveries*: `rate × destinations × response_time`.
Fan-out multiplies.

A profile can also ramp to its rate instead of opening at it:

```yaml
pattern: ramp
pattern_params:
  ramp_duration_seconds: 45
```

The ramp runs from publisher start, so it has to fit inside `warmup` — a spec whose
ramp is still climbing when the window opens is rejected, because its opening samples
would sit below the rate the run reports. Worth using against a deployment that has
been idle: asked for its full rate on the first second it queues while it gets there,
and that queue's wait shows up in the tail of an otherwise clean run.

### Against a deployed app

Everything goes over HTTP, so the same command drives a remote target:

```bash
LOADTEST_URL=https://…  PROM_URL=https://…  ./build/dev/bench.sh spec.yaml
./build/dev/bench.sh --detach spec.yaml   # start it and walk away
./build/dev/bench.sh --report <run_id>    # render it afterwards
```

`--detach` matters for long windows — otherwise the operator's machine has to stay awake
for the whole run. `LOADTEST_TOKEN` sends a bearer header.

## What gets measured

Every event carries four timestamps: **t0** when it was *scheduled* to be sent, **t1**
when the request went out, **t2** the publish ack, **t3** first byte at the destination.
Publish latency is t2−t0, delivery latency t3−t0, generator lag t1−t0.

Measuring from t0 rather than t1 is deliberate: a generator that falls behind reports its
own delay instead of hiding it. Delivery ending at first byte is what makes the receiver's
response time a swept *input* — it spends concurrency without inflating the result.

Distributions go to Prometheus as native histograms. Exact counts stay in an in-process
ledger, because a counter is only observed when a scrape succeeds and `rate()`
extrapolates — the identity `published == completed + missing + cutoff` cannot be built
on an estimate, and that identity is the void check.

## `app/` — Go load test app

A single binary that is both the event publisher and the mock webhook receiver, so every
published event can be reconciled against its delivery, and so end-to-end latency needs no
clock synchronisation between machines. Control-plane REST API, dashboard, and mock
receiver share one port (`:9090`).

Built for multi-tenant, long-running runs against Outpost Cloud: tenants and destinations
are provisioned through the Outpost API, publish rate and mock latency/error rate are
adjustable mid-flight, and per-event status is tracked in a per-group event log.

```bash
make up/loadtest    # Air hot-reload on the shared outpost docker network
```

Dashboard at http://localhost:9090. See [app/](./app/).

The dashboard's ad-hoc groups are for exploration. A run whose numbers might be published
goes through `make bench` instead — only that path produces a ledger and a spec-backed
export.

## `report/` — figures

`fetch.py` folds Prometheus series into a run export, leaving it self-contained.
`make_charts.py --data <export>` renders from it; without `--data` it renders a synthetic
sample marked `SAMPLE DATA`.

Which panels exist is derived from the spec: a profile joins a sweep when it differs from
the baseline in exactly one dimension, so a run with no payload sweep emits no payload
panel, and a caption cannot claim a payload the run did not use.

## `k6/` — k6 scenario suite

The original TypeScript/k6 suite: scenario + environment config, local (minikube) and AWS
(EKS) infrastructure, and a Grafana dashboard.

```bash
cd loadtest/k6
```

See [k6/docs/](./k6/docs/) and `contributing/loadtest/overview.md`.

## Which to use

`make bench` for anything measured. `app/` directly for exploration, throughput ceilings,
and mid-run parameter changes. `k6/` for scripted scenario runs on the existing
AWS/minikube infrastructure.
