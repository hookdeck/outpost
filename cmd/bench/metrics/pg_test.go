package metrics

import (
	"context"
	"os"
	"testing"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/pglogstore"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newPGBench(tb testing.TB) driver.Metrics {
	tb.Helper()

	pgURL := os.Getenv("BENCH_PG_URL")
	if pgURL == "" {
		tb.Skip("BENCH_PG_URL not set — skipping PG metrics benchmarks")
	}

	ctx := context.Background()

	// Run migrations.
	m, err := migrator.New(migrator.MigrationOpts{
		PG: migrator.MigrationOptsPG{URL: pgURL},
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

	db, err := pgxpool.New(ctx, pgURL)
	if err != nil {
		tb.Fatalf("pgxpool: %v", err)
	}
	tb.Cleanup(db.Close)

	return pglogstore.NewLogStore(db)
}

func BenchmarkPGEventMetrics(b *testing.B) {
	store := newPGBench(b)
	benchmarkEventMetrics(b, store)
}

func BenchmarkPGAttemptMetrics(b *testing.B) {
	store := newPGBench(b)
	benchmarkAttemptMetrics(b, store)
}
