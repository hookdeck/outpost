# Metrics Benchmarks Makefile
# Usage: cd cmd/bench/metrics && make <target>

OUTPOST_ROOT := $(shell git rev-parse --show-toplevel)
PG_URL       := postgres://outpost:outpost@localhost:5488/bench?sslmode=disable
CH_ADDR      := localhost:9009
CH_DB        := bench
PG_ROWS      := 10000000
CH_ROWS      := 10000000

# ── Infra ────────────────────────────────────────────────────────────────────

up/pg:
	docker compose -f pg/docker-compose.yml up -d

up/ch:
	docker compose -f ch/docker-compose.yml up -d

up: up/pg up/ch

down/pg:
	docker compose -f pg/docker-compose.yml down -v

down/ch:
	docker compose -f ch/docker-compose.yml down -v

down: down/pg down/ch

# ── Migrations ───────────────────────────────────────────────────────────────

migrate/pg:
	cd $(OUTPOST_ROOT) && BENCH_PG_URL="$(PG_URL)" \
	  go test -run='^$$' -bench=BenchmarkPG -benchtime=1x ./cmd/bench/metrics/

migrate/ch:
	cd $(OUTPOST_ROOT) && BENCH_CH_ADDR="$(CH_ADDR)" BENCH_CH_DB="$(CH_DB)" \
	  go test -run='^$$' -bench=BenchmarkCH -benchtime=1x ./cmd/bench/metrics/

migrate: migrate/pg migrate/ch

# ── Seeding ──────────────────────────────────────────────────────────────────

seed/pg:
	psql "$(PG_URL)" -v ROWS=$(PG_ROWS) -f pg/seed.sql

seed/ch:
	clickhouse client --port 9009 --database $(CH_DB) \
	  --param_rows $(CH_ROWS) < ch/seed.sql

# ── Benchmarks ───────────────────────────────────────────────────────────────

bench/pg:
	cd $(OUTPOST_ROOT) && BENCH_PG_URL="$(PG_URL)" \
	  go test -bench=BenchmarkPG -benchtime=1x -count=1 -timeout=30m ./cmd/bench/metrics/

bench/ch:
	cd $(OUTPOST_ROOT) && BENCH_CH_ADDR="$(CH_ADDR)" BENCH_CH_DB="$(CH_DB)" \
	  go test -bench=BenchmarkCH -benchtime=1x -count=1 -timeout=30m ./cmd/bench/metrics/

bench/pg/sustained:
	cd $(OUTPOST_ROOT) && BENCH_PG_URL="$(PG_URL)" \
	  go test -bench=BenchmarkPG -benchtime=10s -count=3 -timeout=30m ./cmd/bench/metrics/

bench/ch/sustained:
	cd $(OUTPOST_ROOT) && BENCH_CH_ADDR="$(CH_ADDR)" BENCH_CH_DB="$(CH_DB)" \
	  go test -bench=BenchmarkCH -benchtime=10s -count=3 -timeout=30m ./cmd/bench/metrics/

bench: bench/pg bench/ch

# ── Full workflow ────────────────────────────────────────────────────────────

setup/pg: up/pg migrate/pg seed/pg
setup/ch: up/ch migrate/ch seed/ch
setup: setup/pg setup/ch

reset/pg: down/pg up/pg migrate/pg seed/pg
reset/ch: down/ch up/ch migrate/ch seed/ch

.PHONY: up/pg up/ch up down/pg down/ch down \
        migrate/pg migrate/ch migrate \
        seed/pg seed/ch \
        bench/pg bench/ch bench/pg/sustained bench/ch/sustained bench \
        setup/pg setup/ch setup reset/pg reset/ch
