package redistenantstore_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/tenantstore/redistenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// interleave runs inject once, before or after the first command (or
// pipeline) of a hooked client that match selects, so a test can place a
// concurrent write at an exact point of an operation.
type interleave struct {
	mu     sync.Mutex
	armed  bool
	fired  bool
	before bool
	match  func(cmds []goredis.Cmder) bool
	inject func()
}

func (h *interleave) take(cmds []goredis.Cmder) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.armed || h.fired || !h.match(cmds) {
		return false
	}
	h.fired = true
	return true
}

func (h *interleave) run(cmds []goredis.Cmder, next func() error) error {
	if !h.take(cmds) {
		return next()
	}
	if h.before {
		h.inject()
		return next()
	}
	err := next()
	h.inject()
	return err
}

func (h *interleave) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (h *interleave) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		return h.run([]goredis.Cmder{cmd}, func() error { return next(ctx, cmd) })
	}
}

func (h *interleave) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		return h.run(cmds, func() error { return next(ctx, cmds) })
	}
}

// hasCommand matches a command named name (lower case) on key.
func hasCommand(name, key string) func([]goredis.Cmder) bool {
	return func(cmds []goredis.Cmder) bool {
		for _, cmd := range cmds {
			args := cmd.Args()
			if strings.ToLower(cmd.Name()) == name && len(args) > 1 && fmt.Sprint(args[1]) == key {
				return true
			}
		}
		return false
	}
}

// isScriptOn matches a script call whose first key is key.
func isScriptOn(key string) func([]goredis.Cmder) bool {
	return func(cmds []goredis.Cmder) bool {
		for _, cmd := range cmds {
			args := cmd.Args()
			name := strings.ToLower(cmd.Name())
			if (name == "evalsha" || name == "eval") && len(args) > 3 && fmt.Sprint(args[3]) == key {
				return true
			}
		}
		return false
	}
}

// racingStores returns a store whose client runs h, and another store on
// the same server for the injected writes.
func racingStores(t *testing.T, h *interleave, opts ...redistenantstore.Option) (store, other driver.TenantStore) {
	t.Helper()
	mr := miniredis.RunT(t)
	hooked := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	plain := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { hooked.Close(); plain.Close() })
	hooked.AddHook(h)
	return redistenantstore.New(hooked, opts...), redistenantstore.New(plain, opts...)
}

func newMCP(tenantID string) models.Destination {
	return testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithTenantID(tenantID),
		testutil.DestinationFactory.WithType("mcp"),
		testutil.DestinationFactory.WithTopics([]string{"order.created"}),
	)
}

// A create committing between DeleteTenant's read of the summary and its
// write is deleted too.
func TestDeleteTenantDeletesACreateRacingIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := &interleave{match: hasCommand("hgetall", "tenant:{t}:destinations")}
	store, other := racingStores(t, h, redistenantstore.WithTypeLimits(map[string]int{"mcp": 10}))
	principal := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:alice", Max: 10})

	require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: "t"}))
	first := newMCP("t")
	require.NoError(t, store.CreateDestination(ctx, first, principal))
	late := newMCP("t")
	h.inject = func() { require.NoError(t, other.CreateDestination(ctx, late, principal)) }
	h.armed = true

	require.NoError(t, store.DeleteTenant(ctx, "t"))
	require.True(t, h.fired)

	for _, d := range []models.Destination{first, late} {
		_, err := store.RetrieveDestination(ctx, "t", d.ID)
		assert.ErrorIs(t, err, driver.ErrDestinationDeleted, d.ID)
	}
	require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: "t"}))
	one := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:alice", Max: 1})
	require.NoError(t, store.CreateDestination(ctx, newMCP("t"), one), "the buckets went with the tenant")
}

// The delete of a subscription without expiry removes its index entries
// after the delete itself: a subscription created again in between keeps
// them, as both generations have the same member and score.
func TestDeleteKeepsTheIndexOfARecreatedDestination(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := &interleave{before: true, match: isScriptOn("destination_index:mcp")}
	opts := []redistenantstore.Option{redistenantstore.WithIndexedTypes("mcp")}
	store, other := racingStores(t, h, opts...)

	d := newMCP("t")
	require.NoError(t, store.CreateDestination(ctx, d))
	h.inject = func() { require.NoError(t, other.CreateDestination(ctx, d)) }
	h.armed = true

	res, err := store.DeleteDestinationIf(ctx, "t", d.ID, driver.DeleteCondition{Reason: driver.DeleteReasonUnsubscribed})
	require.NoError(t, err)
	require.True(t, res.Deleted)
	require.True(t, h.fired)

	got, err := store.RetrieveDestination(ctx, "t", d.ID)
	require.NoError(t, err)
	require.NotNil(t, got, "created again")
	want := []driver.IndexedDestination{{TenantID: "t", DestinationID: d.ID, Score: driver.NoExpiryScore}}
	for _, topic := range []string{"", "order.created"} {
		entries, err := store.ListIndexedDestinations(ctx, "mcp", topic, driver.NoExpiryScore, 10)
		require.NoError(t, err)
		assert.Equal(t, want, entries, "index %q", topic)
	}
}

// The other order: the removal runs between the create's index write and
// the create itself, while the destination is still deleted.
func TestCreateKeepsItsIndexAgainstALateRemoval(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := &interleave{match: hasCommand("zadd", "destination_index:mcp")}
	opts := []redistenantstore.Option{redistenantstore.WithIndexedTypes("mcp")}
	store, other := racingStores(t, h, opts...)

	d := newMCP("t")
	require.NoError(t, store.CreateDestination(ctx, d))
	res, err := store.DeleteDestinationIf(ctx, "t", d.ID, driver.DeleteCondition{Reason: driver.DeleteReasonUnsubscribed})
	require.NoError(t, err)
	require.True(t, res.Deleted)

	ref := driver.IndexedDestination{TenantID: "t", DestinationID: d.ID, Score: driver.NoExpiryScore}
	h.inject = func() { require.NoError(t, other.RemoveIndexedDestination(ctx, "mcp", d.Topics, ref)) }
	h.armed = true

	require.NoError(t, store.CreateDestination(ctx, d))
	require.True(t, h.fired)
	for _, topic := range []string{"", "order.created"} {
		entries, err := store.ListIndexedDestinations(ctx, "mcp", topic, driver.NoExpiryScore, 10)
		require.NoError(t, err)
		assert.Equal(t, []driver.IndexedDestination{ref}, entries, "index %q", topic)
	}
}
