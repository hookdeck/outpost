package pglogstore

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/drivertest"
	"github.com/hookdeck/outpost/internal/migrator"
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
	m, err := migrator.New(migrator.MigrationOpts{
		PG: migrator.MigrationOptsPG{
			URL: pgURL,
		},
	})
	require.NoError(t, err)
	_, _, err = m.Up(ctx, -1)
	require.NoError(t, err)

	defer func() {
		sourceErr, dbErr := m.Close(ctx)
		require.NoError(t, sourceErr)
		require.NoError(t, dbErr)
	}()

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
