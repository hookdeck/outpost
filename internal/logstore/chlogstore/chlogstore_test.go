package chlogstore

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/drivertest"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/require"
)

// func TestConformance(t *testing.T) {
// 	testutil.CheckIntegrationTest(t)
// 	t.Parallel()

// 	drivertest.RunConformanceTests(t, newHarness)
// }

type harness struct {
	chDB   clickhouse.DB
	closer func()
}

func setupClickHouseConnection(t *testing.T) clickhouse.DB {
	t.Helper()
	t.Cleanup(testinfra.Start(t))

	chConfig := testinfra.NewClickHouseConfig(t)

	chDB, err := clickhouse.New(&chConfig)
	require.NoError(t, err)

	ctx := context.Background()
	m, err := migrator.New(migrator.MigrationOpts{
		CH: migrator.MigrationOptsCH{
			Addr:     chConfig.Addr,
			Username: chConfig.Username,
			Password: chConfig.Password,
			Database: chConfig.Database,
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

	return chDB
}

func newHarness(_ context.Context, t *testing.T) (drivertest.Harness, error) {
	t.Helper()

	chDB := setupClickHouseConnection(t)

	return &harness{
		chDB: chDB,
		closer: func() {
			chDB.Close()
		},
	}, nil
}

func (h *harness) Close() {
	h.closer()
}

func (h *harness) MakeDriver(ctx context.Context) (driver.LogStore, error) {
	return NewLogStore(h.chDB), nil
}
