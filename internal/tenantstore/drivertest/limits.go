package drivertest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLimits(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	newStore := func(t *testing.T, opts DriverOptions) (context.Context, driver.TenantStore) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)
		store, err := h.MakeDriverWithOptions(ctx, opts)
		require.NoError(t, err)
		return ctx, store
	}
	webhook := func(tenantID string) models.Destination {
		return testutil.DestinationFactory.Any(testutil.DestinationFactory.WithTenantID(tenantID))
	}
	requireLimit := func(t *testing.T, err error, bucket string, max int) {
		t.Helper()
		var limitErr *driver.ErrLimitReached
		require.True(t, errors.As(err, &limitErr), "want *ErrLimitReached, got %v", err)
		assert.Equal(t, bucket, limitErr.Bucket)
		assert.Equal(t, max, limitErr.Max)
	}

	t.Run("TypeLimitsSeparateFromGeneral", func(t *testing.T) {
		ctx, store := newStore(t, DriverOptions{MaxDest: 2, TypeLimits: map[string]int{"mcp": 3}})
		tenant := testutil.TenantFactory.Any()
		require.NoError(t, store.UpsertTenant(ctx, tenant))

		webhooks := []models.Destination{webhook(tenant.ID), webhook(tenant.ID)}
		for _, d := range webhooks {
			require.NoError(t, store.CreateDestination(ctx, d))
		}
		require.ErrorIs(t, store.CreateDestination(ctx, webhook(tenant.ID)), driver.ErrMaxDestinationsPerTenantReached)

		mcps := make([]models.Destination, 3)
		for i := range mcps {
			mcps[i] = newMCPDestination(tenant.ID)
			require.NoError(t, store.CreateDestination(ctx, mcps[i]), "mcp does not count toward the general limit")
		}
		requireLimit(t, store.CreateDestination(ctx, newMCPDestination(tenant.ID)), "type:mcp", 3)
		require.ErrorIs(t, store.CreateDestination(ctx, webhook(tenant.ID)), driver.ErrMaxDestinationsPerTenantReached)

		got, err := store.RetrieveTenant(ctx, tenant.ID)
		require.NoError(t, err)
		assert.Equal(t, 5, got.DestinationsCount, "the tenant count includes every type")

		t.Run("a duplicate is reported as such at the limit", func(t *testing.T) {
			require.ErrorIs(t, store.CreateDestination(ctx, mcps[0]), driver.ErrDuplicateDestination)
		})

		t.Run("refresh at the limit", func(t *testing.T) {
			later := time.Now().Add(time.Hour)
			refreshed := mcps[0]
			refreshed.ExpiresAt = &later
			_, err := store.UpdateDestinationIfLive(ctx, refreshed, refreshed.CreatedAt)
			require.NoError(t, err)
		})

		t.Run("deletes free their own slots", func(t *testing.T) {
			res, err := store.DeleteDestinationIf(ctx, tenant.ID, mcps[0].ID, driver.DeleteCondition{})
			require.NoError(t, err)
			require.True(t, res.Deleted)
			require.ErrorIs(t, store.CreateDestination(ctx, webhook(tenant.ID)), driver.ErrMaxDestinationsPerTenantReached)
			require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenant.ID)))

			require.NoError(t, store.DeleteDestination(ctx, tenant.ID, webhooks[0].ID))
			requireLimit(t, store.CreateDestination(ctx, newMCPDestination(tenant.ID)), "type:mcp", 3)
			require.NoError(t, store.CreateDestination(ctx, webhook(tenant.ID)))
		})
	})

	t.Run("Buckets", func(t *testing.T) {
		ctx, store := newStore(t, DriverOptions{TypeLimits: map[string]int{"mcp": 100}})
		tenantID := idgen.String()
		alice := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:alice", Max: 2})
		bob := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:bob", Max: 2})

		first := newMCPDestination(tenantID)
		require.NoError(t, store.CreateDestination(ctx, first, alice))
		require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenantID), alice))
		requireLimit(t, store.CreateDestination(ctx, newMCPDestination(tenantID), alice), "mcp_principal:alice", 2)
		require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenantID), bob), "buckets are independent")

		t.Run("refused create leaves nothing behind", func(t *testing.T) {
			d := newMCPDestination(tenantID)
			requireLimit(t, store.CreateDestination(ctx, d, alice), "mcp_principal:alice", 2)
			got, err := store.RetrieveDestination(ctx, tenantID, d.ID)
			require.NoError(t, err)
			assert.Nil(t, got)
		})

		t.Run("the type bucket is not duplicated", func(t *testing.T) {
			d := newMCPDestination(tenantID)
			require.NoError(t, store.CreateDestination(ctx, d, driver.WithBuckets(driver.Bucket{Name: "type:mcp", Max: 1})))
		})

		t.Run("updates keep the membership", func(t *testing.T) {
			first.Metadata = map[string]string{"k": "v"}
			_, err := store.UpdateDestinationIfLive(ctx, first, first.CreatedAt)
			require.NoError(t, err)
			require.NoError(t, store.UpsertDestination(ctx, first))

			res, err := store.DeleteDestinationIf(ctx, tenantID, first.ID, driver.DeleteCondition{})
			require.NoError(t, err)
			require.True(t, res.Deleted)
			require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenantID), alice), "the delete freed the slot")
			requireLimit(t, store.CreateDestination(ctx, newMCPDestination(tenantID), alice), "mcp_principal:alice", 2)
		})

		t.Run("unlimited bucket", func(t *testing.T) {
			for range 3 {
				require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenantID),
					driver.WithBuckets(driver.Bucket{Name: "tracked", Max: 0})))
			}
		})

		t.Run("validation", func(t *testing.T) {
			assert.Error(t, store.CreateDestination(ctx, newMCPDestination(tenantID), driver.WithBuckets(driver.Bucket{Name: ""})))
			assert.Error(t, store.CreateDestination(ctx, newMCPDestination(tenantID), driver.WithBuckets(driver.Bucket{Name: "a\x00b"})))
			assert.Error(t, store.CreateDestination(ctx, newMCPDestination(tenantID),
				driver.WithBuckets(driver.Bucket{Name: "x", Max: 1}, driver.Bucket{Name: "x", Max: 2})))
		})
	})

	t.Run("DeleteTenantResetsBuckets", func(t *testing.T) {
		ctx, store := newStore(t, DriverOptions{MaxDest: 1, TypeLimits: map[string]int{"mcp": 1}})
		tenant := testutil.TenantFactory.Any()
		require.NoError(t, store.UpsertTenant(ctx, tenant))
		principal := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:alice", Max: 1})
		require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenant.ID), principal))
		require.NoError(t, store.CreateDestination(ctx, webhook(tenant.ID)))

		require.NoError(t, store.DeleteTenant(ctx, tenant.ID))
		require.NoError(t, store.UpsertTenant(ctx, tenant))

		require.NoError(t, store.CreateDestination(ctx, newMCPDestination(tenant.ID), principal))
		require.NoError(t, store.CreateDestination(ctx, webhook(tenant.ID)))
	})

	t.Run("ConcurrentCreatesNeverExceedLimits", func(t *testing.T) {
		const workers = 24
		ctx, store := newStore(t, DriverOptions{MaxDest: 4, TypeLimits: map[string]int{"mcp": 6}})
		tenantID := idgen.String()

		run := func(create func() error) (ok int, errs []error) {
			var mu sync.Mutex
			var wg sync.WaitGroup
			for range workers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					err := create()
					mu.Lock()
					defer mu.Unlock()
					if err == nil {
						ok++
					} else {
						errs = append(errs, err)
					}
				}()
			}
			wg.Wait()
			return ok, errs
		}
		countType := func(typ string) int {
			list, err := store.ListDestination(ctx, driver.ListDestinationRequest{TenantID: tenantID, Type: []string{typ}})
			require.NoError(t, err)
			return len(list)
		}

		t.Run("general limit", func(t *testing.T) {
			ok, errs := run(func() error { return store.CreateDestination(ctx, webhook(tenantID)) })
			assert.Equal(t, 4, ok)
			for _, err := range errs {
				require.ErrorIs(t, err, driver.ErrMaxDestinationsPerTenantReached)
			}
			assert.Equal(t, 4, countType("webhook"))
		})

		t.Run("bucket limit", func(t *testing.T) {
			principal := driver.WithBuckets(driver.Bucket{Name: "mcp_principal:alice", Max: 3})
			ok, errs := run(func() error { return store.CreateDestination(ctx, newMCPDestination(tenantID), principal) })
			assert.Equal(t, 3, ok)
			for _, err := range errs {
				requireLimit(t, err, "mcp_principal:alice", 3)
			}
		})

		t.Run("type limit", func(t *testing.T) {
			ok, errs := run(func() error { return store.CreateDestination(ctx, newMCPDestination(tenantID)) })
			assert.Equal(t, 3, ok, "6 per tenant, 3 already taken")
			for _, err := range errs {
				requireLimit(t, err, "type:mcp", 6)
			}
			assert.Equal(t, 6, countType("mcp"))
		})

		t.Run("same ID", func(t *testing.T) {
			other := idgen.String()
			d := newMCPDestination(other)
			ok, errs := run(func() error { return store.CreateDestination(ctx, d) })
			assert.Equal(t, 1, ok)
			for _, err := range errs {
				require.ErrorIs(t, err, driver.ErrDuplicateDestination)
			}
		})
	})
}
