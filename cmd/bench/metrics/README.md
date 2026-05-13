# Metrics Benchmarks

Benchmarks `QueryEventMetrics` / `QueryAttemptMetrics` against PostgreSQL and ClickHouse.

Shared test cases in `bench_test.go`, backend-specific setup in `pg_test.go` / `ch_test.go`.

## Quick Start

```bash
cd outpost/cmd/bench/metrics

# ── ClickHouse ───────────────────────────────
make setup/ch                    # up + migrate + seed (10M)
make bench/ch                    # single iteration
make bench/ch/sustained          # 10s x 3 runs
make down/ch                     # cleanup

# ── PostgreSQL ───────────────────────────────
make setup/pg                    # up + migrate + seed (10M)
make bench/pg                    # single iteration
make bench/pg/sustained          # 10s x 3 runs
make down/pg                     # cleanup

# ── Both ─────────────────────────────────────
make setup                       # setup both
make bench                       # bench both
make down                        # cleanup both
```

### Individual steps

```bash
make up/ch          # start container
make migrate/ch     # run migrations
make seed/ch        # seed data (default 10M, override: make seed/ch CH_ROWS=1000000000)
make bench/ch       # run benchmarks
make reset/ch       # down + up + migrate + seed (fresh start)
```

Same targets available for `/pg`.

## Structure

```
metrics/
  Makefile          # all commands
  bench_test.go     # shared test cases + date ranges + helpers
  pg_test.go        # PG setup (BENCH_PG_URL)
  ch_test.go        # CH setup (BENCH_CH_ADDR)
  pg/               # PG infra (docker-compose, seed.sql)
  ch/               # CH infra (docker-compose, seed.sql, config/)
```

## Data Distribution

Deterministic via modulo arithmetic (identical for both backends):

- **2 tenants** — `tenant_0` (90%), `tenant_1` (10%)
- **500 destinations** — `dest_0` through `dest_499`
- **3 topics** — `order.created`, `order.updated`, `payment.received`
- **Time** — evenly spread across January 2000
- **Attempts** — chained retries (1 event -> 1-4 attempts), 0.5% permanently failed
- 10M events -> ~12.6M attempts (22.6M total rows)
