package migration_004_escape_ids

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/tenantstore/redistenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nopLogger struct{}

func (nopLogger) Verbose() bool                { return false }
func (nopLogger) LogProgress(int, int, string) {}
func (nopLogger) LogInfo(string)               {}
func (nopLogger) LogDebug(string)              {}
func (nopLogger) LogWarning(string)            {}
func (nopLogger) LogError(string, error)       {}

// fixture is the tenants and destination IDs each test starts from.
var fixture = map[string][]string{
	// "e%3Af" is the escaped form of "e:f".
	"plain":            {"des_1", "a:b", "x{y}", "e:f", "e%3Af"},
	"org:1":            {"des_1", "c:d"},
	"x":                {"des_1"},
	"x}:destination:y": {"d"},
	// The escaped form of "a:b" is itself a tenant ID.
	"a:b":   {"des_1", "c"},
	"a%3Ab": {"des_2"},
}

// seedOldFormat writes the fixture through the store, then renames every key
// back to the unescaped format, so the records are exactly what the store
// writes.
func seedOldFormat(t *testing.T, client redis.Client, deploymentID string) (driver.TenantStore, *EscapeIDsMigration) {
	t.Helper()
	ctx := context.Background()
	store := redistenantstore.New(client, redistenantstore.WithDeploymentID(deploymentID))
	m := New(client, nopLogger{}, deploymentID)

	for tenantID, destinationIDs := range fixture {
		require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: tenantID, CreatedAt: time.Now()}))
		for _, destinationID := range destinationIDs {
			require.NoError(t, store.CreateDestination(ctx, testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithID(destinationID),
				testutil.DestinationFactory.WithTenantID(tenantID),
			)))
		}
	}

	// Rename in two steps: an old key of one tenant can equal a new key of
	// another.
	renames := map[string]string{}
	for tenantID, destinationIDs := range fixture {
		renames[m.newTenantKey(tenantID)] = m.oldTenantKey(tenantID)
		renames[m.newSummaryKey(tenantID)] = m.oldSummaryKey(tenantID)
		for _, destinationID := range destinationIDs {
			renames[m.newDestinationKey(tenantID, destinationID)] = m.oldDestinationKey(tenantID, destinationID)
		}
	}
	for from := range renames {
		require.NoError(t, client.Rename(ctx, from, "tmp:"+from).Err())
	}
	for from, to := range renames {
		require.NoError(t, client.Rename(ctx, "tmp:"+from, to).Err())
	}
	return store, m
}

func assertFixtureReadable(t *testing.T, store driver.TenantStore) {
	t.Helper()
	ctx := context.Background()
	for tenantID, destinationIDs := range fixture {
		tenant, err := store.RetrieveTenant(ctx, tenantID)
		require.NoError(t, err, tenantID)
		require.NotNil(t, tenant, tenantID)
		assert.Equal(t, tenantID, tenant.ID)
		assert.Equal(t, len(destinationIDs), tenant.DestinationsCount, tenantID)

		for _, destinationID := range destinationIDs {
			destination, err := store.RetrieveDestination(ctx, tenantID, destinationID)
			require.NoError(t, err, "%s / %s", tenantID, destinationID)
			require.NotNil(t, destination, "%s / %s", tenantID, destinationID)
			assert.Equal(t, destinationID, destination.ID)
		}
	}
}

func runMigration(t *testing.T, m *EscapeIDsMigration) {
	t.Helper()
	ctx := context.Background()
	plan, err := m.Plan(ctx)
	require.NoError(t, err)
	state, err := m.Apply(ctx, plan)
	require.NoError(t, err)
	assert.Zero(t, state.Progress.FailedItems)
}

func TestEscapeIDsMigration(t *testing.T) {
	t.Parallel()

	for _, deploymentID := range []string{"", "dp_001"} {
		t.Run("deployment="+deploymentID, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			client := testutil.CreateTestRedisClient(t)
			store, m := seedOldFormat(t, client, deploymentID)

			// Before the migration the store can't find a tenant with a
			// colon in its ID.
			tenant, err := store.RetrieveTenant(ctx, "org:1")
			require.NoError(t, err)
			require.Nil(t, tenant)

			runMigration(t, m)
			assertFixtureReadable(t, store)

			result, err := m.Verify(ctx, nil)
			require.NoError(t, err)
			assert.True(t, result.Valid, result.Issues)

			// Nothing of the old format is left, and a second run is a no-op.
			exists, err := client.Exists(ctx, m.oldTenantKey("org:1"), m.oldSummaryKey("org:1"), m.oldDestinationKey("plain", "a:b")).Result()
			require.NoError(t, err)
			assert.Zero(t, exists)

			plan, err := m.Plan(ctx)
			require.NoError(t, err)
			assert.Zero(t, plan.EstimatedItems)
		})
	}
}

func TestEscapeIDsMigration_ResumesAfterPartialRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)
	store, m := seedOldFormat(t, client, "")

	plan, err := m.Plan(ctx)
	require.NoError(t, err)
	moves := plan.Data.([]move)
	require.Greater(t, len(moves), 4)

	// Stop at every possible point, then run again from a fresh plan.
	for stop := 1; stop < len(moves); stop++ {
		_, err := m.rename(ctx, moves[stop-1])
		require.NoError(t, err)

		rest, err := m.Plan(ctx)
		require.NoError(t, err)
		assert.Equal(t, len(moves)-stop, rest.EstimatedItems, "after %d of %d moves", stop, len(moves))
	}

	runMigration(t, m)
	assertFixtureReadable(t, store)
}

func TestEscapeIDsMigration_KeepsExpiry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)
	store, m := seedOldFormat(t, client, "")

	require.NoError(t, client.Expire(ctx, m.oldTenantKey("org:1"), time.Hour).Err())

	runMigration(t, m)

	ttl, err := client.TTL(ctx, m.newTenantKey("org:1")).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, 50*time.Minute)
	_ = store
}

func TestEscapeIDsMigration_CopyAndDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)
	_, m := seedOldFormat(t, client, "")

	from, to := m.oldTenantKey("org:1"), m.newTenantKey("org:1")
	before, err := client.HGetAll(ctx, from).Result()
	require.NoError(t, err)
	require.NoError(t, client.Expire(ctx, from, time.Hour).Err())

	moved, err := m.copyAndDelete(ctx, move{from: from, to: to})
	require.NoError(t, err)
	assert.True(t, moved)

	after, err := client.HGetAll(ctx, to).Result()
	require.NoError(t, err)
	assert.Equal(t, before, after)
	ttl, err := client.TTL(ctx, to).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, 50*time.Minute)
	exists, err := client.Exists(ctx, from).Result()
	require.NoError(t, err)
	assert.Zero(t, exists)
}

func TestEscapeIDsMigration_StopsAtFailedRename(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)
	store, m := seedOldFormat(t, client, "")

	// Another live record sits where a destination of "org:1" has to go.
	blocker := m.newDestinationKey("org:1", "des_1")
	require.NoError(t, client.HSet(ctx, blocker, "id", "other", "entity", "destination").Err())

	plan, err := m.Plan(ctx)
	require.NoError(t, err)
	_, err = m.Apply(ctx, plan)
	require.Error(t, err)

	// The tenant's record has not moved, so the tenant is still pending.
	exists, err := client.Exists(ctx, m.oldTenantKey("org:1")).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists)
	result, err := m.Verify(ctx, nil)
	require.NoError(t, err)
	assert.False(t, result.Valid)

	require.NoError(t, client.Del(ctx, blocker).Err())
	runMigration(t, m)
	assertFixtureReadable(t, store)
}

func TestEscapeIDsMigration_ReplacesDeletedRecordAtTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)
	store, m := seedOldFormat(t, client, "")

	// A deleted destination "c" of tenant "a%3Ab" is under an old key that is
	// also the new key of destination "c" of tenant "a:b".
	orphan := m.oldDestinationKey("a%3Ab", "c")
	require.Equal(t, m.newDestinationKey("a:b", "c"), orphan)
	require.NoError(t, client.HSet(ctx, orphan, "id", "c", "entity", "destination", "deleted_at", "1700000000000").Err())
	require.NoError(t, client.Expire(ctx, orphan, time.Hour).Err())

	runMigration(t, m)
	assertFixtureReadable(t, store)
}

func TestEscapeIDsMigration_FinishesInterruptedCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)
	store, m := seedOldFormat(t, client, "")

	// A copy that stopped before deleting the old key: both keys hold the record.
	for _, mv := range []move{
		{from: m.oldTenantKey("org:1"), to: m.newTenantKey("org:1")},
		{from: m.oldSummaryKey("org:1"), to: m.newSummaryKey("org:1")},
	} {
		fields, err := client.HGetAll(ctx, mv.from).Result()
		require.NoError(t, err)
		values := make([]interface{}, 0, len(fields)*2)
		for field, value := range fields {
			values = append(values, field, value)
		}
		require.NoError(t, client.HSet(ctx, mv.to, values...).Err())
	}

	runMigration(t, m)
	assertFixtureReadable(t, store)

	exists, err := client.Exists(ctx, m.oldTenantKey("org:1"), m.oldSummaryKey("org:1"), m.oldDestinationKey("org:1", "des_1")).Result()
	require.NoError(t, err)
	assert.Zero(t, exists)
}

// crossSlotClient refuses every rename the way Redis Cluster does when the two
// keys hash to different slots.
type crossSlotClient struct {
	redis.Client
}

func (c crossSlotClient) RenameNX(ctx context.Context, key, newkey string) *goredis.BoolCmd {
	cmd := goredis.NewBoolCmd(ctx)
	cmd.SetErr(errors.New("CROSSSLOT Keys in request don't hash to the same slot"))
	return cmd
}

func TestEscapeIDsMigration_CrossSlot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)
	store, _ := seedOldFormat(t, client, "")
	m := New(crossSlotClient{client}, nopLogger{}, "")

	runMigration(t, m)
	assertFixtureReadable(t, store)

	plan, err := m.Plan(ctx)
	require.NoError(t, err)
	assert.Zero(t, plan.EstimatedItems)
}
