# Metrics Seed & QA Scripts

Scripts for seeding metrics data and QA-testing the portal's metrics dashboard.

## Overview

The metrics dashboard shows 5 charts per destination (events count, delivery events, error rate, by status code, by topic) plus a sparkline per row in the destinations list. All charts use the attempts endpoint (`/metrics/attempts`).

These scripts generate realistic event→attempt chains in the local Postgres DB so you can visually verify the dashboard across different scenarios.

## Scripts

### `seed_metrics.sh`

Generates seed data (events + attempts) with configurable error rates, retry chains, and time distribution.

```bash
./scripts/metrics/seed_metrics.sh              # seed with defaults (10k events, 30d)
./scripts/metrics/seed_metrics.sh --dry-run    # print SQL only
./scripts/metrics/seed_metrics.sh --clean      # delete seed data only
```

All seeded rows use `seed_` ID prefix for easy cleanup. Key tunables via env vars:

| Env var | Default | Description |
|---------|---------|-------------|
| `SEED_EVENTS` | 10000 | Number of events |
| `SEED_DAYS` | 30 | Spread over N days (0 = last hour) |
| `SEED_ERROR_RATE` | 0.35 | Failure rate per attempt |
| `SEED_RETRY_FRAC` | 0.4 | Fraction of events that get retries |
| `SEED_MAX_RETRIES` | 3 | Max retry attempts per event (1-3) |
| `SEED_TOPICS` | order.completed,... | Comma-separated topics |
| `SEED_CODES` | 500,422 | Failure HTTP codes |
| `SEED_DESTINATIONS` | auto-detected | Falls back to `des_test` |

### `qa_metrics.sh`

Wraps `seed_metrics.sh` with named scenarios and verification checklists.

```bash
./scripts/metrics/qa_metrics.sh              # list scenarios
./scripts/metrics/qa_metrics.sh healthy      # run one scenario
./scripts/metrics/qa_metrics.sh all          # walk through all interactively
```

Each scenario cleans existing seed data, seeds fresh data, and prints what to verify in the portal.

## Scenarios

| Scenario | Events | Error rate | What it tests |
|----------|--------|------------|---------------|
| `healthy` | 10k | 5% | Baseline — smooth charts, events ≈ deliveries |
| `failing` | 750 | 85% | Destination down — deliveries >> events |
| `spike` | 1k | 60% | Volume spike 3 days ago |
| `empty` | 0 | — | Empty state rendering |
| `single` | 1 | 0% | Minimal data edge case |
| `all-fail` | 500 | 100% | All red, 100% error rate |
| `all-success` | 500 | 0% | All green, 0% error rate |
| `recent` | 60 | 30% | Last hour only (1m granularity) |
| `many-topics` | 1k | 30% | 10 topics in breakdown table |
| `many-codes` | 1k | 50% | 9 HTTP status codes |
| `retry-heavy` | 500 | 40% | Events vs deliveries gap (2-3x) |

## Requirements

- Outpost running locally via `docker compose`
- Portal at `http://localhost:3333`
- A destination must exist (scripts default to `des_test`)
