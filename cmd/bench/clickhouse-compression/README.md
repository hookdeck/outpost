# ClickHouse Compression Bench

One-off bench used to validate enabling LZ4 client compression (#909 / #911).

Alternates `Compression` off / LZ4 / ZSTD against the same CH instance, tags
each insert with a `query_id`, then pulls exact wire bytes from
`system.query_log.ProfileEvents['NetworkReceiveBytes']` for comparison.

## Run

```bash
# Local dev CH (build/dev/deps/compose.yml --profile clickhouse)
CLICKHOUSE_ADDR=localhost:29000 \
CLICKHOUSE_USERNAME=outpost \
CLICKHOUSE_PASSWORD=outpost \
CLICKHOUSE_DATABASE=outpost \
  go run ./cmd/bench/clickhouse-compression --batches=5 --rows=100000

# Remote (CH Cloud, TLS)
CLICKHOUSE_ADDR=<host>:9440 \
CLICKHOUSE_USERNAME=<user> \
CLICKHOUSE_PASSWORD=<pass> \
CLICKHOUSE_DATABASE=<db> \
CLICKHOUSE_TLS_ENABLED=true \
  go run ./cmd/bench/clickhouse-compression --batches=10 --rows=10000
```

## Flags

| flag | default | meaning |
|---|---|---|
| `--batches` | 5 | runs per mode |
| `--rows` | 10000 | rows per batch |
| `--modes` | `off,lz4,zstd` | subset of codecs to test |
| `--keep` | false | keep the throwaway table on exit |

The bench creates a `bench_compression_<rand>` MergeTree table and drops it
on exit (no schema/migration dependency). Payload is ~700-byte JSON
representative of event bodies.
