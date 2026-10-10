package redistenantstore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/tenantstore/redistenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hashTag returns the part of key Redis Cluster hashes: the first non-empty
// {...} section, else the whole key.
func hashTag(key string) string {
	if start := strings.IndexByte(key, '{'); start >= 0 {
		if end := strings.IndexByte(key[start+1:], '}'); end > 0 {
			return key[start+1 : start+1+end]
		}
	}
	return key
}

// slotRecorder records the keys of every script and transaction, which must
// all hash to one slot to run on Redis Cluster.
type slotRecorder struct {
	mu         sync.Mutex
	violations []string
	scripts    int
	txs        int
}

func (r *slotRecorder) check(kind string, keys []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range keys[1:] {
		if hashTag(key) != hashTag(keys[0]) {
			r.violations = append(r.violations, fmt.Sprintf("%s mixes %q and %q", kind, keys[0], key))
		}
	}
}

func (r *slotRecorder) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (r *slotRecorder) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		name := strings.ToLower(cmd.Name())
		if (name == "eval" || name == "evalsha") && len(args) >= 3 {
			n, _ := strconv.Atoi(fmt.Sprint(args[2]))
			keys := make([]string, n)
			for i := range keys {
				keys[i] = fmt.Sprint(args[3+i])
			}
			if n > 0 {
				r.check("script", keys)
			}
			r.mu.Lock()
			r.scripts++
			r.mu.Unlock()
		}
		return next(ctx, cmd)
	}
}

func (r *slotRecorder) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		if len(cmds) > 0 && strings.ToLower(cmds[0].Name()) == "multi" {
			var keys []string
			for _, cmd := range cmds {
				switch strings.ToLower(cmd.Name()) {
				case "multi", "exec":
				default:
					keys = append(keys, fmt.Sprint(cmd.Args()[1]))
				}
			}
			if len(keys) > 0 {
				r.check("transaction", keys)
			}
			r.mu.Lock()
			r.txs++
			r.mu.Unlock()
		}
		return next(ctx, cmds)
	}
}

// exercise runs every multi-key operation of the store.
func exercise(t *testing.T, store driver.TenantStore, tenantID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: tenantID}))
	require.NoError(t, store.WriteFence(ctx, tenantID, "mcp_revoked:x", now.Add(-time.Hour), time.Minute))

	d := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithTenantID(tenantID),
		testutil.DestinationFactory.WithType("mcp"),
		testutil.DestinationFactory.WithTopics([]string{"order.created"}),
		testutil.DestinationFactory.WithExpiresAt(now.Add(time.Hour)),
		testutil.DestinationFactory.WithDisabledAt(now),
	)
	require.NoError(t, store.CreateDestination(ctx, d,
		driver.WithBuckets(driver.Bucket{Name: "mcp_principal:p", Max: 5}),
		driver.WithNotDeletedSince(now), driver.WithFence("mcp_revoked:x")))
	webhook := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenantID))
	require.NoError(t, store.CreateDestination(ctx, webhook))
	require.NoError(t, store.UpsertDestination(ctx, webhook))

	res, err := store.ParkRetry(ctx, tenantID, d.ID, "r1", 10, now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, driver.ParkResultParked, res)
	d.DisabledAt = nil
	upd, err := store.UpdateDestinationIfLive(ctx, d, d.CreatedAt,
		driver.WithResumeParkedRetries(), driver.WithNotDeletedSince(now), driver.WithFence("mcp_revoked:x"))
	require.NoError(t, err)
	require.NotEmpty(t, upd.ResumeKey)
	_, err = store.PopResumeMembers(ctx, tenantID, upd.ResumeKey, 10)
	require.NoError(t, err)
	_, err = store.DisableDestination(ctx, tenantID, d.ID, now)
	require.NoError(t, err)
	_, err = store.DeleteDestinationIf(ctx, tenantID, d.ID, driver.DeleteCondition{Type: "mcp", Reason: driver.DeleteReasonRevoked})
	require.NoError(t, err)
	require.NoError(t, store.CreateDestination(ctx, d))
	require.NoError(t, store.DeleteDestination(ctx, tenantID, webhook.ID))
	require.NoError(t, store.DeleteTenant(ctx, tenantID))
}

func TestMultiKeyOperationsStayInTheTenantSlot(t *testing.T) {
	t.Parallel()

	for _, deploymentID := range []string{"", "dp_001"} {
		t.Run("deployment "+deploymentID, func(t *testing.T) {
			mr := miniredis.RunT(t)
			client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { client.Close() })
			recorder := &slotRecorder{}
			client.AddHook(recorder)

			store := redistenantstore.New(client,
				redistenantstore.WithDeploymentID(deploymentID),
				redistenantstore.WithTypeLimits(map[string]int{"mcp": 10, "other": 3}),
				redistenantstore.WithIndexedTypes("mcp"),
			)
			exercise(t, store, "tenant-"+idgen.String())

			assert.Empty(t, recorder.violations)
			assert.NotZero(t, recorder.scripts)
			assert.NotZero(t, recorder.txs)
		})
	}
}

func TestKeyLayout(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	store := redistenantstore.New(client,
		redistenantstore.WithDeploymentID("dp"),
		redistenantstore.WithTypeLimits(map[string]int{"mcp": 10}),
		redistenantstore.WithIndexedTypes("mcp"),
	)

	tenantID := "t1"
	require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: tenantID}))
	d := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("sub_1"),
		testutil.DestinationFactory.WithTenantID(tenantID),
		testutil.DestinationFactory.WithType("mcp"),
		testutil.DestinationFactory.WithTopics([]string{"order.created"}),
		testutil.DestinationFactory.WithDisabledAt(time.Now()),
	)
	require.NoError(t, store.CreateDestination(ctx, d, driver.WithBuckets(driver.Bucket{Name: "mcp_principal:p", Max: 5})))
	require.NoError(t, store.WriteFence(ctx, tenantID, "mcp_revoked:p", time.Now(), time.Minute))
	_, err := store.ParkRetry(ctx, tenantID, d.ID, "r1", 10, time.Now().Add(time.Hour))
	require.NoError(t, err)

	keys := mr.Keys()
	assert.ElementsMatch(t, []string{
		"dp:tenant:{t1}:tenant",
		"dp:tenant:{t1}:destinations",
		"dp:tenant:{t1}:destination:sub_1",
		"dp:tenant:{t1}:buckets",
		"dp:tenant:{t1}:bucket:type:mcp",
		"dp:tenant:{t1}:bucket:mcp_principal:p",
		"dp:tenant:{t1}:fence:mcp_revoked:p",
		"dp:tenant:{t1}:parked_retries:sub_1",
		"dp:destination_index:mcp",
		"dp:destination_index:mcp:topic:order.created",
		"dp:destination_index:mcp:topics",
	}, keys)

	// The Redis migrations scan tenant:*:destination:* for destination hashes.
	migrationGlob := regexp.MustCompile(`^dp:tenant:.*:destination:.*$`)
	for _, key := range keys {
		if migrationGlob.MatchString(key) {
			assert.Equal(t, "dp:tenant:{t1}:destination:sub_1", key, "only destination hashes match the migration glob")
		}
	}

	t.Run("buckets recorded on the destination", func(t *testing.T) {
		recorded := mr.HGet("dp:tenant:{t1}:destination:sub_1", "buckets")
		assert.JSONEq(t, `["type:mcp","mcp_principal:p"]`, recorded)
	})

	t.Run("index member and score", func(t *testing.T) {
		score, err := mr.ZScore("dp:destination_index:mcp", driver.IndexMember(tenantID, d.ID))
		require.NoError(t, err)
		assert.Equal(t, float64(driver.NoExpiryScore), score)
		assert.Equal(t, `["t1","sub_1"]`, driver.IndexMember(tenantID, d.ID))
	})
}

func TestExpiresAtFailsClosed(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	store := redistenantstore.New(client)

	d := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Hour)))
	require.NoError(t, store.CreateDestination(ctx, d))
	mr.HSet(fmt.Sprintf("tenant:{%s}:destination:%s", d.TenantID, d.ID), "expires_at", "not-a-time")

	got, err := store.RetrieveDestination(ctx, d.TenantID, d.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ExpiresAt)
	assert.True(t, got.IsExpired(time.Now()), "an unreadable expiry means expired")
	assert.False(t, got.MatchEvent(testutil.EventFactory.Any(testutil.EventFactory.WithTenantID(d.TenantID)), true))

	now := time.Now()
	res, err := store.DeleteDestinationIf(ctx, d.TenantID, d.ID, driver.DeleteCondition{ExpiredBefore: &now})
	require.NoError(t, err)
	assert.True(t, res.Deleted, "the sweeper deletes it")
}

func TestPartialHashDoesNotPanic(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	store := redistenantstore.New(client)

	for _, credentials := range []string{"", "short"} {
		mr.HSet("tenant:{t}:destination:d", "id", "d", "type", "webhook", "created_at", "1", "credentials", credentials)
		_, err := store.RetrieveDestination(context.Background(), "t", "d")
		assert.Error(t, err)
	}
}

func TestMatchEventDecodesFiltersLazily(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	store := redistenantstore.New(client)

	summaryKey := "tenant:{t}:destinations"
	// Entries as written by earlier releases (no expires_at) and entries
	// whose filter is not an object, which must only fail when evaluated.
	mr.HSet(summaryKey,
		"legacy", `{"id":"legacy","type":"webhook","topics":["user.created"],"filter":{"data":{"a":1}},"disabled":false}`,
		"bad_other_topic", `{"id":"bad_other_topic","type":"webhook","topics":["user.deleted"],"filter":"oops","disabled":false}`,
		"bad_disabled", `{"id":"bad_disabled","type":"webhook","topics":["user.created"],"filter":[1],"disabled":true}`,
		"bad_expired", `{"id":"bad_expired","type":"webhook","topics":["user.created"],"filter":[1],"disabled":false,"expires_at":1}`,
		"mcp_wildcard", `{"id":"mcp_wildcard","type":"mcp","topics":["*"],"filter":[1],"disabled":false}`,
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID("t"),
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithData(json.RawMessage(`{"a":1}`)),
	)
	matched, err := store.MatchEvent(ctx, event, true)
	require.NoError(t, err)
	assert.Equal(t, []driver.MatchedDestination{{ID: "legacy", Type: "webhook"}}, matched)

	mr.HSet(summaryKey, "bad_match", `{"id":"bad_match","type":"webhook","topics":["user.created"],"filter":"oops","disabled":false}`)
	_, err = store.MatchEvent(ctx, event, true)
	assert.Error(t, err, "a filter that needs evaluating must decode")
}

func TestCreateDropsStaleBucketMembers(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	store := redistenantstore.New(client, redistenantstore.WithTypeLimits(map[string]int{"mcp": 2}))

	// Members left by an interrupted cleanup: not in the summary.
	_, err := mr.SAdd("tenant:{t}:bucket:type:mcp", "ghost_1", "ghost_2")
	require.NoError(t, err)
	_, err = mr.SAdd("tenant:{t}:bucket:mcp_principal:p", "ghost_3")
	require.NoError(t, err)

	principal := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:p", Max: 1})
	d := testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID("t"), testutil.DestinationFactory.WithType("mcp"))
	require.NoError(t, store.CreateDestination(ctx, d, principal))

	members, err := mr.Members("tenant:{t}:bucket:type:mcp")
	require.NoError(t, err)
	assert.Equal(t, []string{d.ID}, members)
	members, err = mr.Members("tenant:{t}:bucket:mcp_principal:p")
	require.NoError(t, err)
	assert.Equal(t, []string{d.ID}, members)

	var limitErr *driver.ErrLimitReached
	err = store.CreateDestination(ctx, testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID("t"), testutil.DestinationFactory.WithType("mcp")), principal)
	require.ErrorAs(t, err, &limitErr)
	assert.Equal(t, "mcp_principal:p", limitErr.Bucket)
}

func TestIndexDeploymentIsolation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)
	store1 := redistenantstore.New(client, redistenantstore.WithDeploymentID("dp_001"), redistenantstore.WithIndexedTypes("mcp"))
	store2 := redistenantstore.New(client, redistenantstore.WithDeploymentID("dp_002"), redistenantstore.WithIndexedTypes("mcp"))

	d := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("mcp"),
		testutil.DestinationFactory.WithTopics([]string{"order.created"}),
	)
	require.NoError(t, store1.CreateDestination(ctx, d))

	for _, topic := range []string{"", "order.created"} {
		entries, err := store1.ListIndexedDestinations(ctx, "mcp", topic, driver.NoExpiryScore, 10)
		require.NoError(t, err)
		assert.Len(t, entries, 1)
		entries, err = store2.ListIndexedDestinations(ctx, "mcp", topic, driver.NoExpiryScore, 10)
		require.NoError(t, err)
		assert.Empty(t, entries)
	}
	topics, err := store2.ListIndexedTopics(ctx, "mcp")
	require.NoError(t, err)
	assert.Empty(t, topics)

	// A fence of one deployment doesn't fence the other.
	require.NoError(t, store1.WriteFence(ctx, d.TenantID, "f", time.Now(), time.Minute))
	require.NoError(t, store2.CreateDestination(ctx, d, driver.WithNotDeletedSince(time.Now().Add(-time.Minute)), driver.WithFence("f")))
}
