package pglogstore

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/drivertest"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
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

func (h *harness) FlushWrites(ctx context.Context) error {
	// PostgreSQL is immediately consistent, no flush needed
	return nil
}

func (h *harness) MakeDriver(ctx context.Context) (driver.LogStore, error) {
	return NewLogStore(h.db), nil
}

func TestListLatestAttempts_UndecodableRowFailsOwnKey(t *testing.T) {
	testutil.CheckIntegrationTest(t)
	t.Parallel()

	db := setupPGConnection(t)
	defer db.Close()
	ctx := context.Background()
	store := NewLogStore(db)

	tenantID, destID := "bad_tenant", "bad_dest"
	t0 := time.Now().Truncate(time.Second).Add(-10 * time.Minute)
	var entries []*models.LogEntry
	for _, id := range []string{"bad_evt_ok", "bad_evt_bad"} {
		entries = append(entries, &models.LogEntry{
			Event: testutil.EventFactory.AnyPointer(
				testutil.EventFactory.WithID(id),
				testutil.EventFactory.WithTenantID(tenantID),
				testutil.EventFactory.WithDestinationID(destID),
				testutil.EventFactory.WithTime(t0),
			),
			Attempt: testutil.AttemptFactory.AnyPointer(
				testutil.AttemptFactory.WithID("att_"+id),
				testutil.AttemptFactory.WithTenantID(tenantID),
				testutil.AttemptFactory.WithEventID(id),
				testutil.AttemptFactory.WithDestinationID(destID),
				testutil.AttemptFactory.WithStatus("failed"),
				testutil.AttemptFactory.WithTime(t0),
			),
		})
	}
	require.NoError(t, store.InsertMany(ctx, entries))
	_, err := db.Exec(ctx, `UPDATE attempts SET event_metadata = '{"n":1}' WHERE event_id = 'bad_evt_bad'`)
	require.NoError(t, err)

	keyOK := driver.AttemptKey{TenantID: tenantID, EventID: "bad_evt_ok", DestinationID: destID}
	keyBad := driver.AttemptKey{TenantID: tenantID, EventID: "bad_evt_bad", DestinationID: destID}
	got, err := store.ListLatestAttempts(ctx, []driver.LatestAttemptLookup{
		{Key: keyOK, NotBefore: t0.Add(-time.Second)},
		{Key: keyBad, NotBefore: t0.Add(-time.Second)},
	})

	var keyErrs driver.KeyErrors
	require.ErrorAs(t, err, &keyErrs)
	require.Len(t, keyErrs, 1)
	assert.ErrorContains(t, keyErrs[keyBad], "metadata")
	require.Len(t, got, 1)
	assert.Equal(t, "att_bad_evt_ok", got[keyOK].Attempt.ID)
}
