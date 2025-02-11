package chlogstore

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/drivertest"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

func TestConformance(t *testing.T) {
	testutil.CheckIntegrationTest(t)
	t.Parallel()

	drivertest.RunConformanceTests(t, newHarness)
}

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
	require.NoError(t, chDB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events (
			id String,
			tenant_id String,
			destination_id String,
			topic String,
			eligible_for_retry Bool,
			time DateTime,
			metadata String,
			data String
		)
		ENGINE = MergeTree
		ORDER BY (id, time);
	`))
	require.NoError(t, chDB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deliveries (
			id String,
			delivery_event_id String,
			event_id String,
			destination_id String,
			status String,
			time DateTime
		)
		ENGINE = ReplacingMergeTree
		ORDER BY (id, time);
	`))

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
