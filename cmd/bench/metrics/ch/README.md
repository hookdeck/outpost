# CH Metrics Benchmark

Benchmarks `QueryEventMetrics` / `QueryAttemptMetrics` against ClickHouse (2 CPU, 8GB).

## Prerequisites

- Docker (Compose v2)
- Go 1.24+
- `clickhouse` CLI

## Quick Start

```bash
cd outpost

# 1. Start CH
docker compose -f cmd/bench/metrics/ch/docker-compose.yml up -d

# 2. Run migrations
BENCH_CH_ADDR="localhost:9009" \
  go test -run='^$' -bench=BenchmarkCH -benchtime=1x ./cmd/bench/metrics/

# 3. Seed (default 10M — adjust --param_rows N)
clickhouse client --port 9009 --database bench \
  --param_rows 10000000 < cmd/bench/metrics/ch/seed.sql

# 4a. Single iteration
BENCH_CH_ADDR="localhost:9009" \
  go test -bench=BenchmarkCH -benchtime=1x -count=1 -timeout=30m ./cmd/bench/metrics/

# 4b. Sustained (10s x 3 runs)
BENCH_CH_ADDR="localhost:9009" \
  go test -bench=BenchmarkCH -benchtime=10s -count=3 -timeout=30m ./cmd/bench/metrics/

# 5. Cleanup
docker compose -f cmd/bench/metrics/ch/docker-compose.yml down -v
```

## Re-seeding

```bash
docker compose -f cmd/bench/metrics/ch/docker-compose.yml down -v
docker compose -f cmd/bench/metrics/ch/docker-compose.yml up -d
# Repeat steps 2-4
```

## Data Distribution

Deterministic via modulo arithmetic (shared with PG bench):

- **2 tenants** — `tenant_0` (90%), `tenant_1` (10%)
- **500 destinations** — `dest_0` through `dest_499`
- **3 topics** — `order.created`, `order.updated`, `payment.received`
- **Time** — evenly spread across January 2000
- **Attempts** — chained retries (1 event -> 1-4 attempts), 0.5% permanently failed
- 10M events -> ~12.6M attempts

## Resource Tuning

| Setting | Default | Purpose |
|---------|---------|---------|
| CPUs | 2 | Parallel query threads |
| Memory | 8GB | Container limit |
| max_memory_usage | 6GB | Per-query memory limit |
| max_threads | 2 | Query parallelism |
