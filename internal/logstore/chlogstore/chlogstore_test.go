package chlogstore

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/drivertest"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/pagination"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConformance(t *testing.T) {
	testutil.CheckIntegrationTest(t)
	t.Parallel()

	drivertest.RunConformanceTests(t, newHarness)
}

type harness struct {
	chDB         clickhouse.DB
	deploymentID string
	closer       func()
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

func (h *harness) FlushWrites(ctx context.Context) error {
	// Force ClickHouse to merge parts and deduplicate rows on both tables
	eventsTable := "events"
	attemptsTable := "attempts"
	if h.deploymentID != "" {
		eventsTable = h.deploymentID + "_events"
		attemptsTable = h.deploymentID + "_attempts"
	}
	if err := h.chDB.Exec(ctx, "OPTIMIZE TABLE "+eventsTable+" FINAL"); err != nil {
		return err
	}
	return h.chDB.Exec(ctx, "OPTIMIZE TABLE "+attemptsTable+" FINAL")
}

func (h *harness) MakeDriver(ctx context.Context) (driver.LogStore, error) {
	return NewLogStore(h.chDB, h.deploymentID), nil
}

func TestConformance_WithDeploymentID(t *testing.T) {
	testutil.CheckIntegrationTest(t)
	t.Parallel()

	drivertest.RunConformanceTests(t, newHarnessWithDeploymentID)
}

func newHarnessWithDeploymentID(ctx context.Context, t *testing.T) (drivertest.Harness, error) {
	t.Helper()

	chDB := setupClickHouseConnectionWithDeploymentID(t, "mydeployment")

	return &harness{
		chDB:         chDB,
		deploymentID: "mydeployment",
		closer: func() {
			chDB.Close()
		},
	}, nil
}

// TestEventDedup verifies that duplicate event rows are prevented at the write
// path and deduplicated at the read path. The dataset models a realistic webhook
// delivery scenario:
//
// ── Dataset ──────────────────────────────────────────────────────────────────
//
// Event A ("order.created"): Fanout to 3 destinations in one batch.
//
//	Batch 1:  dest-a #1 success, dest-b #1 failed, dest-c #1 success
//	Batch 2:  dest-b #2 success  (retry)
//
// Event B ("user.signup"): Single destination, fails twice then succeeds.
//
//	Batch 3:  dest-a #1 failed
//	Batch 4:  dest-a #2 failed   (retry)
//	Batch 5:  dest-a #3 success  (retry)
//
// Event C ("payment.received"): Single destination, succeeds first try.
//
//	Batch 6:  dest-b #1 success
//
// ── Expected ─────────────────────────────────────────────────────────────────
//
//	Event rows:   3 (one per unique event, retries skipped by write path)
//	Attempt rows: 8 (A×4 + B×3 + C×1, all persisted)
//	ListEvent:    3 unique events (client-side dedup hides any stragglers)
//
// After injecting legacy duplicates (raw batch inserts simulating pre-fix data):
//
//	Event rows:   9 (3 original + 6 injected)
//	ListEvent:    still 3 (read-path dedup)
func TestEventDedup(t *testing.T) {
	testutil.CheckIntegrationTest(t)
	t.Parallel()

	ctx := context.Background()
	chDB := setupClickHouseConnection(t)
	defer chDB.Close()

	logStore := NewLogStore(chDB, "")

	tenantID := "dedup-tenant"
	baseTime := time.Now().Truncate(time.Second)

	eventA := &models.Event{
		ID: "evt-a", TenantID: tenantID,
		MatchedDestinationIDs: []string{"dest-a", "dest-b", "dest-c"},
		Topic:                 "order.created", EligibleForRetry: true,
		Time: baseTime, Data: []byte(`{"order_id":"100"}`),
	}
	eventB := &models.Event{
		ID: "evt-b", TenantID: tenantID,
		MatchedDestinationIDs: []string{"dest-a"},
		Topic:                 "user.signup", EligibleForRetry: true,
		Time: baseTime.Add(-1 * time.Minute), Data: []byte(`{"user_id":"42"}`),
	}
	eventC := &models.Event{
		ID: "evt-c", TenantID: tenantID,
		MatchedDestinationIDs: []string{"dest-b"},
		Topic:                 "payment.received", EligibleForRetry: false,
		Time: baseTime.Add(-2 * time.Minute), Data: []byte(`{"amount":99}`),
	}

	att := func(id, eventID, destID string, num int, status string, t time.Time) *models.Attempt {
		return &models.Attempt{
			ID: id, TenantID: tenantID, EventID: eventID,
			DestinationID: destID, AttemptNumber: num, Status: status, Time: t,
		}
	}

	// Batch 1: Event A fanout — 3 destinations
	require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{
		{Event: eventA, Attempt: att("att-a1", "evt-a", "dest-a", 1, "success", baseTime)},
		{Event: eventA, Attempt: att("att-a2", "evt-a", "dest-b", 1, "failed", baseTime)},
		{Event: eventA, Attempt: att("att-a3", "evt-a", "dest-c", 1, "success", baseTime)},
	}))
	// Batch 2: Event A retry for dest-b
	require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{
		{Event: eventA, Attempt: att("att-a4", "evt-a", "dest-b", 2, "success", baseTime.Add(time.Second))},
	}))
	// Batch 3-5: Event B — fails, retries, succeeds
	require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{
		{Event: eventB, Attempt: att("att-b1", "evt-b", "dest-a", 1, "failed", baseTime.Add(-1*time.Minute))},
	}))
	require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{
		{Event: eventB, Attempt: att("att-b2", "evt-b", "dest-a", 2, "failed", baseTime.Add(-1*time.Minute+time.Second))},
	}))
	require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{
		{Event: eventB, Attempt: att("att-b3", "evt-b", "dest-a", 3, "success", baseTime.Add(-1*time.Minute+2*time.Second))},
	}))
	// Batch 6: Event C — single success
	require.NoError(t, logStore.InsertMany(ctx, []*models.LogEntry{
		{Event: eventC, Attempt: att("att-c1", "evt-c", "dest-b", 1, "success", baseTime.Add(-2*time.Minute))},
	}))

	// ── Write-path: raw row counts ──────────────────────────────────────

	var eventRows uint64
	row := chDB.QueryRow(ctx, "SELECT count() FROM events WHERE tenant_id = ?", tenantID)
	require.NoError(t, row.Scan(&eventRows))
	assert.Equal(t, uint64(3), eventRows, "retries should not re-insert event rows")

	var attemptRows uint64
	row = chDB.QueryRow(ctx, "SELECT count() FROM attempts WHERE tenant_id = ?", tenantID)
	require.NoError(t, row.Scan(&attemptRows))
	assert.Equal(t, uint64(8), attemptRows, "all attempts persisted")

	// ── Read-path: ListEvent deduplicates ────────────────────────────────

	startTime := baseTime.Add(-10 * time.Minute)
	resp, err := logStore.ListEvent(ctx, driver.ListEventRequest{
		TenantIDs:  []string{tenantID},
		Limit:      100,
		TimeFilter: driver.TimeFilter{GTE: &startTime},
	})
	require.NoError(t, err)
	assert.Len(t, resp.Data, 3, "ListEvent returns 3 unique events")

	seen := map[string]bool{}
	for _, evt := range resp.Data {
		assert.False(t, seen[evt.ID], "duplicate in ListEvent: %s", evt.ID)
		seen[evt.ID] = true
	}
	assert.True(t, seen["evt-a"])
	assert.True(t, seen["evt-b"])
	assert.True(t, seen["evt-c"])

	// ── Read-path with legacy duplicates ─────────────────────────────────
	// Inject duplicate event rows via raw batch inserts (simulates pre-fix data).

	for range 3 {
		batch, batchErr := chDB.PrepareBatch(ctx, `INSERT INTO events (event_id, tenant_id, matched_destination_ids, topic, eligible_for_retry, event_time, metadata, data)`)
		require.NoError(t, batchErr)
		require.NoError(t, batch.Append("evt-a", tenantID, []string{"dest-a", "dest-b", "dest-c"}, "order.created", true, baseTime, "{}", `{"order_id":"100"}`))
		require.NoError(t, batch.Append("evt-b", tenantID, []string{"dest-a"}, "user.signup", true, baseTime.Add(-1*time.Minute), "{}", `{"user_id":"42"}`))
		require.NoError(t, batch.Send())
	}

	row = chDB.QueryRow(ctx, "SELECT count() FROM events WHERE tenant_id = ?", tenantID)
	require.NoError(t, row.Scan(&eventRows))
	assert.Equal(t, uint64(9), eventRows, "3 original + 6 injected legacy duplicates")

	resp, err = logStore.ListEvent(ctx, driver.ListEventRequest{
		TenantIDs:  []string{tenantID},
		Limit:      100,
		TimeFilter: driver.TimeFilter{GTE: &startTime},
	})
	require.NoError(t, err)
	assert.Len(t, resp.Data, 3, "client-side dedup hides legacy rows")
}

// TestFetchAndDedupTruncation verifies that fetchAndDedup never returns more
// items than the requested limit. When duplicates cause the first batch to
// yield fewer unique items, the next batch may overshoot the limit without
// truncation.
//
// Setup: 1 event duplicated 2× plus 3 unique events. With limit=2 and DESC:
//
//	Batch 1: [A, A]        → dedup → [A]          (1 < 2, loop continues)
//	Batch 2: [B, C]        → dedup → [A, B, C]    (3 items — exceeds limit)
func TestFetchAndDedupTruncation(t *testing.T) {
	testutil.CheckIntegrationTest(t)
	t.Parallel()

	ctx := context.Background()
	chDB := setupClickHouseConnection(t)
	defer chDB.Close()

	tenantID := "dedup-truncation"
	baseTime := time.Now().Truncate(time.Second)

	insertRawEvent := func(id string, eventTime time.Time) {
		batch, err := chDB.PrepareBatch(ctx, `INSERT INTO events (event_id, tenant_id, matched_destination_ids, topic, eligible_for_retry, event_time, metadata, data)`)
		require.NoError(t, err)
		require.NoError(t, batch.Append(id, tenantID, []string{"dest-1"}, "test.topic", true, eventTime, "{}", `{}`))
		require.NoError(t, batch.Send())
	}

	// Two rows for event A (same ID = duplicate in separate parts)
	insertRawEvent("evt-trunc-a", baseTime)
	insertRawEvent("evt-trunc-a", baseTime)
	// Unique events at decreasing times
	insertRawEvent("evt-trunc-b", baseTime.Add(-1*time.Second))
	insertRawEvent("evt-trunc-c", baseTime.Add(-2*time.Second))
	insertRawEvent("evt-trunc-d", baseTime.Add(-3*time.Second))

	startTime := baseTime.Add(-10 * time.Minute)
	limit := 2
	result, err := fetchAndDedup(ctx, chDB, pagination.QueryInput{
		Limit:   limit,
		Compare: "<",
		SortDir: "desc",
	}, func(qi pagination.QueryInput) (string, []any) {
		return buildEventQuery("events", driver.ListEventRequest{
			TenantIDs:  []string{tenantID},
			TimeFilter: driver.TimeFilter{GTE: &startTime},
		}, qi)
	}, scanEvents, func(e eventWithPosition) string {
		return e.Event.ID
	}, eventWithPosition.cursorPosition)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result), limit,
		"fetchAndDedup must not return more items than the requested limit")
}

func setupClickHouseConnectionWithDeploymentID(t *testing.T, deploymentID string) clickhouse.DB {
	t.Helper()
	t.Cleanup(testinfra.Start(t))

	chConfig := testinfra.NewClickHouseConfig(t)

	chDB, err := clickhouse.New(&chConfig)
	require.NoError(t, err)

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

	defer func() {
		sourceErr, dbErr := m.Close(ctx)
		require.NoError(t, sourceErr)
		require.NoError(t, dbErr)
	}()

	return chDB
}
