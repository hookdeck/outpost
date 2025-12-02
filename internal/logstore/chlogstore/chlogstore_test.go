package chlogstore

import (
	"context"
	"fmt"
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

	chDB := setupClickHouseConnection(t)
	deploymentID := "test_deployment"

	// Create the deployment-specific table
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS event_log_%s (
			event_id String,
			tenant_id String,
			destination_id String,
			topic String,
			eligible_for_retry Bool,
			event_time DateTime64(3),
			metadata String,
			data String,
			delivery_id String,
			delivery_event_id String,
			status String,
			delivery_time DateTime64(3),
			code String,
			response_data String,
			INDEX idx_topic topic TYPE bloom_filter GRANULARITY 4,
			INDEX idx_status status TYPE set(100) GRANULARITY 4
		) ENGINE = MergeTree
		PARTITION BY toYYYYMMDD(event_time)
		ORDER BY (tenant_id, destination_id, event_time, event_id, delivery_time)
	`, deploymentID)

	err := chDB.Exec(context.Background(), createTableSQL)
	require.NoError(t, err)

	return &harnessWithDeployment{
		chDB:         chDB,
		deploymentID: deploymentID,
		closer: func() {
			// Drop the deployment-specific table
			chDB.Exec(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS event_log_%s", deploymentID))
			chDB.Close()
		},
	}, nil
}

type harnessWithDeployment struct {
	chDB         clickhouse.DB
	deploymentID string
	closer       func()
}

func (h *harnessWithDeployment) Close() {
	h.closer()
}

func (h *harnessWithDeployment) MakeDriver(ctx context.Context) (driver.LogStore, error) {
	return NewLogStore(h.chDB, h.deploymentID)
}
