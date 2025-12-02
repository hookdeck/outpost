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

func TestConformance(t *testing.T) {
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
	return NewLogStore(h.chDB, "")
}

func TestConformance_WithDeploymentID(t *testing.T) {
	t.Parallel()

	drivertest.RunConformanceTests(t, newHarnessWithDeploymentID)
}

func newHarnessWithDeploymentID(_ context.Context, t *testing.T) (drivertest.Harness, error) {
	t.Helper()

	t.Cleanup(testinfra.Start(t))

	chConfig := testinfra.NewClickHouseConfig(t)
	deploymentID := "test_deployment"

	chDB, err := clickhouse.New(&chConfig)
	require.NoError(t, err)

	// Use the migrator with DeploymentID to create deployment-specific tables
	ctx := context.Background()
	m, err := migrator.New(migrator.MigrationOpts{
		CH: migrator.MigrationOptsCH{
			Addr:         chConfig.Addr,
			Username:     chConfig.Username,
			Password:     chConfig.Password,
			Database:     chConfig.Database,
			DeploymentID: deploymentID,
		},
	})
	require.NoError(t, err)
	_, _, err = m.Up(ctx, -1)
	require.NoError(t, err)

	return &harnessWithDeployment{
		chDB:         chDB,
		deploymentID: deploymentID,
		migrator:     m,
	}, nil
}

type harnessWithDeployment struct {
	chDB         clickhouse.DB
	deploymentID string
	migrator     *migrator.Migrator
}

func (h *harnessWithDeployment) Close() {
	ctx := context.Background()
	// Roll back migrations (drops deployment-specific tables)
	h.migrator.Down(ctx, -1)
	h.migrator.Close(ctx)
	h.chDB.Close()
}

func (h *harnessWithDeployment) MakeDriver(ctx context.Context) (driver.LogStore, error) {
	return NewLogStore(h.chDB, h.deploymentID)
}
