package tenantstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFacadeOptions(t *testing.T) {
	t.Parallel()

	for name, store := range map[string]tenantstore.TenantStore{
		"mem": tenantstore.NewMemTenantStore(
			tenantstore.MemWithMaxDestinationsPerTenant(1),
			tenantstore.MemWithTypeLimits(map[string]int{"mcp": 1}),
			tenantstore.MemWithIndexedTypes("mcp"),
		),
		"redis": tenantstore.New(tenantstore.Config{
			RedisClient:              testutil.CreateTestRedisClient(t),
			MaxDestinationsPerTenant: 1,
			TypeLimits:               map[string]int{"mcp": 1},
			IndexedTypes:             []string{"mcp"},
			DeploymentID:             "dp",
		}),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			newMCP := func() error {
				return store.CreateDestination(ctx, testutil.DestinationFactory.Any(
					testutil.DestinationFactory.WithType("mcp"),
					testutil.DestinationFactory.WithTopics([]string{"order.created"}),
					testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Hour)),
				), tenantstore.WithBuckets(tenantstore.Bucket{Name: "mcp_principal:p", Max: 5}))
			}

			require.NoError(t, store.CreateDestination(ctx, testutil.DestinationFactory.Any()))
			require.ErrorIs(t, store.CreateDestination(ctx, testutil.DestinationFactory.Any()), tenantstore.ErrMaxDestinationsPerTenantReached)
			require.NoError(t, newMCP())

			var limitErr *tenantstore.ErrLimitReached
			require.True(t, errors.As(newMCP(), &limitErr))
			assert.Equal(t, tenantstore.TypeBucket("mcp"), limitErr.Bucket)

			entries, err := store.ListIndexedDestinations(ctx, "mcp", "order.created", tenantstore.NoExpiryScore, 10)
			require.NoError(t, err)
			assert.Len(t, entries, 1)
		})
	}
}
