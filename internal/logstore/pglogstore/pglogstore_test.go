package pglogstore

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/drivertest"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestConformance(t *testing.T) {
	testutil.CheckIntegrationTest(t)
	t.Parallel()

	drivertest.RunConformanceTests(t, newHarness)
}

type harness struct {
	db     *pgxpool.Pool
	closer func()
}

func setupPGConnection(t *testing.T) *pgxpool.Pool {
	t.Helper()
	t.Cleanup(testinfra.Start(t))

	pgURL := testinfra.NewPostgresConfig(t)

	db, err := pgxpool.New(context.Background(), pgURL)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events (
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			destination_id TEXT NOT NULL,
			topic TEXT NOT NULL,
			eligible_for_retry BOOLEAN NOT NULL,
			time TIMESTAMPTZ NOT NULL,
			metadata JSONB NOT NULL,
			data JSONB NOT NULL,
			PRIMARY KEY (id)
		);

		CREATE INDEX IF NOT EXISTS events_tenant_time_idx ON events (tenant_id, time DESC);

		CREATE TABLE IF NOT EXISTS deliveries (
			id TEXT NOT NULL,
			event_id TEXT NOT NULL,
			destination_id TEXT NOT NULL,
			status TEXT NOT NULL,
			time TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (id),
			FOREIGN KEY (event_id) REFERENCES events (id)
		);

		CREATE INDEX IF NOT EXISTS deliveries_event_time_idx ON deliveries (event_id, time DESC);
	`)
	require.NoError(t, err)

	return db
}

func newHarness(_ context.Context, t *testing.T) (drivertest.Harness, error) {
	t.Helper()

	db := setupPGConnection(t)

	return &harness{
		db: db,
		closer: func() {
			db.Close()
		},
	}, nil
}

func (h *harness) Close() {
	h.closer()
}

func (h *harness) MakeDriver(ctx context.Context) (driver.LogStore, error) {
	return NewLogStore(h.db), nil
}
