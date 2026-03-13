# PG Metrics Benchmark

Benchmarks `QueryEventMetrics` / `QueryAttemptMetrics` against PostgreSQL (2 CPU, 8GB).

## Prerequisites

- Docker (Compose v2)
- Go 1.24+
- `psql`

## Quick Start

```bash
cd outpost

# 1. Start PG
docker compose -f cmd/bench/metrics/pg/docker-compose.yml up -d

# 2. Run migrations
BENCH_PG_URL="postgres://outpost:outpost@localhost:5488/bench?sslmode=disable" \
  go test -run='^$' -bench=BenchmarkPG -benchtime=1x ./cmd/bench/metrics/

# 3. Seed (default 10M — adjust -v ROWS=N)
psql "postgres://outpost:outpost@localhost:5488/bench?sslmode=disable" \
  -v ROWS=10000000 -f cmd/bench/metrics/pg/seed.sql

# 4a. Single iteration
BENCH_PG_URL="postgres://outpost:outpost@localhost:5488/bench?sslmode=disable" \
  go test -bench=BenchmarkPG -benchtime=1x -count=1 -timeout=30m ./cmd/bench/metrics/

# 4b. Sustained (10s x 3 runs)
BENCH_PG_URL="postgres://outpost:outpost@localhost:5488/bench?sslmode=disable" \
  go test -bench=BenchmarkPG -benchtime=10s -count=3 -timeout=30m ./cmd/bench/metrics/

# 5. Cleanup
docker compose -f cmd/bench/metrics/pg/docker-compose.yml down -v
```

## Re-seeding

```bash
docker compose -f cmd/bench/metrics/pg/docker-compose.yml down -v
docker compose -f cmd/bench/metrics/pg/docker-compose.yml up -d
# Repeat steps 2-4
```

## Data Distribution

Deterministic via modulo arithmetic (shared with CH bench):

- **2 tenants** — `tenant_0` (90%), `tenant_1` (10%)
- **500 destinations** — `dest_0` through `dest_499`
- **3 topics** — `order.created`, `order.updated`, `payment.received`
- **Time** — evenly spread across January 2000
- **Attempts** — chained retries (1 event -> 1-4 attempts), 0.5% permanently failed
- 10M events -> ~12.6M attempts

## Resource Tuning

| Setting | Default | Purpose |
|---------|---------|---------|
| CPUs | 2 | Parallel query workers |
| Memory | 4GB | Container limit |
| shared_buffers | 1GB | PG buffer pool |
| work_mem | 256MB | Per-sort/hash memory |
| effective_cache_size | 3GB | Planner hint for OS cache |
