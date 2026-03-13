package metrics

import (
	"context"
	"os"
	"testing"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logstore/chlogstore"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/migrator"
)

func newCHBench(tb testing.TB) driver.Metrics {
	tb.Helper()

	chAddr := os.Getenv("BENCH_CH_ADDR")
	if chAddr == "" {
		tb.Skip("BENCH_CH_ADDR not set — skipping CH metrics benchmarks")
	}

	chDB := os.Getenv("BENCH_CH_DB")
	if chDB == "" {
		chDB = "bench"
	}

	ctx := context.Background()

	// Run migrations.
	m, err := migrator.New(migrator.MigrationOpts{
		CH: migrator.MigrationOptsCH{
			Addr:     chAddr,
			Database: chDB,
			Username: "default",
		},
	})
	if err != nil {
		tb.Fatalf("migrator: %v", err)
	}
	_, _, err = m.Up(ctx, -1)
	if err != nil {
		tb.Fatalf("migrate up: %v", err)
	}
	srcErr, dbErr := m.Close(ctx)
	if srcErr != nil {
		tb.Fatalf("migrator close src: %v", srcErr)
	}
	if dbErr != nil {
		tb.Fatalf("migrator close db: %v", dbErr)
	}

	conn, err := clickhouse.New(&clickhouse.ClickHouseConfig{
		Addr:     chAddr,
		Database: chDB,
		Username: "default",
	})
	if err != nil {
		tb.Fatalf("clickhouse: %v", err)
	}
	tb.Cleanup(func() { conn.Close() })

	return chlogstore.NewLogStore(conn, "")
}

func BenchmarkCHEventMetrics(b *testing.B) {
	store := newCHBench(b)
	benchmarkEventMetrics(b, store)
}

func BenchmarkCHAttemptMetrics(b *testing.B) {
	store := newCHBench(b)
	benchmarkAttemptMetrics(b, store)
}
