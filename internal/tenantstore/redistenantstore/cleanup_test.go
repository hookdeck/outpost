package redistenantstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/tenantstore/redistenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMiniredisStore(t *testing.T, opts ...redistenantstore.Option) (*miniredis.Miniredis, driver.TenantStore) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return mr, redistenantstore.New(client, opts...)
}

// Tombstones only keep what the deleted checks read: the configuration,
// secrets, filter and metadata go with the destination.
func TestTombstonesDropThePayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mr, store := newMiniredisStore(t, redistenantstore.WithSecret("test-secret"))
	require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: "t"}))

	full := func() models.Destination {
		return testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID("t"),
			testutil.DestinationFactory.WithType("mcp"),
			testutil.DestinationFactory.WithTopics([]string{"order.created"}),
			testutil.DestinationFactory.WithConfig(map[string]string{"url": "https://example.com/hook", "arguments": `{"total":1}`}),
			testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "whsec_secret"}),
			testutil.DestinationFactory.WithDeliveryMetadata(map[string]string{"Authorization": "Bearer token"}),
			testutil.DestinationFactory.WithMetadata(map[string]string{"plugin": "chatgpt"}),
			testutil.DestinationFactory.WithFilter(models.Filter{"data": map[string]any{"total": 1.0}}),
			testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Hour)),
			testutil.DestinationFactory.WithDisabledAt(time.Now()),
		)
	}
	requireTombstone := func(t *testing.T, d models.Destination, reason string) {
		t.Helper()
		key := "tenant:{t}:destination:" + d.ID
		fields, err := mr.HKeys(key)
		require.NoError(t, err)
		for _, field := range []string{"deleted_at", "deleted_reason", "type", "topics", "created_at", "expires_at"} {
			assert.Contains(t, fields, field)
		}
		for _, field := range []string{"config", "credentials", "delivery_metadata", "metadata", "filter"} {
			assert.NotContains(t, fields, field)
		}
		assert.Equal(t, reason, mr.HGet(key, "deleted_reason"))
		assert.Equal(t, 7*24*time.Hour, mr.TTL(key))
		_, err = store.RetrieveDestination(ctx, "t", d.ID)
		assert.ErrorIs(t, err, driver.ErrDestinationDeleted)
	}

	t.Run("delete", func(t *testing.T) {
		d := full()
		require.NoError(t, store.CreateDestination(ctx, d))
		res, err := store.DeleteDestinationIf(ctx, "t", d.ID, driver.DeleteCondition{Reason: driver.DeleteReasonUnsubscribed})
		require.NoError(t, err)
		require.True(t, res.Deleted)
		assert.Equal(t, "mcp", res.Type)
		assert.Equal(t, []string{"order.created"}, []string(res.Topics))
		assert.Equal(t, d.ExpiresAt.UnixMilli(), res.ExpiresAtMs)
		requireTombstone(t, d, driver.DeleteReasonUnsubscribed)

		t.Run("created again whole", func(t *testing.T) {
			require.NoError(t, store.CreateDestination(ctx, d))
			got, err := store.RetrieveDestination(ctx, "t", d.ID)
			require.NoError(t, err)
			assert.Equal(t, d.Config, got.Config)
			assert.Equal(t, d.Credentials, got.Credentials)
			assert.Equal(t, d.DeliveryMetadata, got.DeliveryMetadata)
			assert.Equal(t, d.Metadata, got.Metadata)
			assert.Equal(t, d.Filter, got.Filter)
		})
	})

	t.Run("tenant delete", func(t *testing.T) {
		d := full()
		require.NoError(t, store.CreateDestination(ctx, d))
		require.NoError(t, store.DeleteTenant(ctx, "t"))
		requireTombstone(t, d, driver.DeleteReasonTenantDeleted)
	})
}

// Deleting the last destination of a bucket removes its name from the
// registry, which would otherwise keep every principal ever seen.
func TestDeletesForgetEmptiedBuckets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mr, store := newMiniredisStore(t, redistenantstore.WithTypeLimits(map[string]int{"mcp": 10}))
	const registry = "tenant:{t}:buckets"
	alice := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:alice", Max: 5})
	bob := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:bob", Max: 5})
	registered := func(t *testing.T) []string {
		t.Helper()
		if !mr.Exists(registry) {
			return nil
		}
		members, err := mr.Members(registry)
		require.NoError(t, err)
		return members
	}

	a1, a2, b1 := newMCP("t"), newMCP("t"), newMCP("t")
	require.NoError(t, store.CreateDestination(ctx, a1, alice))
	require.NoError(t, store.CreateDestination(ctx, a2, alice))
	require.NoError(t, store.CreateDestination(ctx, b1, bob))
	assert.ElementsMatch(t, []string{"type:mcp", "mcp_principal:alice", "mcp_principal:bob"}, registered(t))

	require.NoError(t, store.DeleteDestination(ctx, "t", b1.ID))
	assert.ElementsMatch(t, []string{"type:mcp", "mcp_principal:alice"}, registered(t))

	res, err := store.DeleteDestinationIf(ctx, "t", a1.ID, driver.DeleteCondition{Reason: driver.DeleteReasonRevoked})
	require.NoError(t, err)
	require.True(t, res.Deleted)
	assert.ElementsMatch(t, []string{"type:mcp", "mcp_principal:alice"}, registered(t), "alice still has a2")

	res, err = store.DeleteDestinationIf(ctx, "t", a2.ID, driver.DeleteCondition{Reason: driver.DeleteReasonExpired})
	require.NoError(t, err)
	require.True(t, res.Deleted)
	assert.Empty(t, registered(t))

	require.NoError(t, store.CreateDestination(ctx, newMCP("t"), bob))
	assert.ElementsMatch(t, []string{"type:mcp", "mcp_principal:bob"}, registered(t))
}

// A refused create at a full bucket sweeps it for stale members at most once
// a minute, so repeated refusals stay cheap.
func TestFullBucketSweptAtMostOncePerMinute(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mr, store := newMiniredisStore(t, redistenantstore.WithTypeLimits(map[string]int{"mcp": 2}))
	const bucket = "tenant:{t}:bucket:type:mcp"

	// Members left by an interrupted cleanup: not in the summary.
	_, err := mr.SAdd(bucket, "ghost_1", "ghost_2")
	require.NoError(t, err)
	first := newMCP("t")
	require.NoError(t, store.CreateDestination(ctx, first), "the sweep frees the bucket")

	_, err = mr.SAdd(bucket, "ghost_3")
	require.NoError(t, err)
	var limitErr *driver.ErrLimitReached
	require.ErrorAs(t, store.CreateDestination(ctx, newMCP("t")), &limitErr, "swept less than a minute ago")
	assert.True(t, mr.Exists(bucket))
	ok, err := mr.SIsMember(bucket, "ghost_3")
	require.NoError(t, err)
	assert.True(t, ok, "not swept again")

	mr.FastForward(time.Minute)
	require.NoError(t, store.CreateDestination(ctx, newMCP("t")), "swept again a minute later")
	ok, err = mr.SIsMember(bucket, "ghost_3")
	require.NoError(t, err)
	assert.False(t, ok)
}
