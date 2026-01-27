package redistenantstore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/pagination/paginationtest"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/tenantstore/drivertest"
	"github.com/hookdeck/outpost/internal/tenantstore/redistenantstore"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redisClientFactory is a function that creates a Redis client for testing.
type redisClientFactory func(t *testing.T) redis.Cmdable

// miniredisFactory creates a miniredis client (in-memory, no RediSearch).
func miniredisFactory(t *testing.T) redis.Cmdable {
	return testutil.CreateTestRedisClient(t)
}

// redisStackFactory creates a Redis Stack client (with RediSearch).
func redisStackFactory(t *testing.T) redis.Cmdable {
	testinfra.Start(t)
	redisCfg := testinfra.NewRedisStackConfig(t)
	client, err := redis.New(context.Background(), redisCfg)
	if err != nil {
		t.Fatalf("failed to create redis client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// dragonflyFactory creates a Dragonfly client (no RediSearch).
func dragonflyFactory(t *testing.T) redis.Cmdable {
	testinfra.Start(t)
	redisCfg := testinfra.NewDragonflyConfig(t)
	client, err := redis.New(context.Background(), redisCfg)
	if err != nil {
		t.Fatalf("failed to create dragonfly client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// dragonflyStackFactory creates a Dragonfly client on DB 0 (with RediSearch).
func dragonflyStackFactory(t *testing.T) redis.Cmdable {
	testinfra.Start(t)
	redisCfg := testinfra.NewDragonflyStackConfig(t)
	client, err := redis.New(context.Background(), redisCfg)
	if err != nil {
		t.Fatalf("failed to create dragonfly stack client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// redisTenantStoreHarness implements drivertest.Harness for Redis-backed stores.
type redisTenantStoreHarness struct {
	factory      redisClientFactory
	t            *testing.T
	deploymentID string
}

func (h *redisTenantStoreHarness) MakeDriver(ctx context.Context) (driver.TenantStore, error) {
	client := h.factory(h.t)
	s := redistenantstore.New(client,
		redistenantstore.WithSecret("test-secret"),
		redistenantstore.WithAvailableTopics(testutil.TestTopics),
		redistenantstore.WithDeploymentID(h.deploymentID),
	)
	if err := s.Init(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (h *redisTenantStoreHarness) MakeDriverWithMaxDest(ctx context.Context, maxDest int) (driver.TenantStore, error) {
	client := h.factory(h.t)
	s := redistenantstore.New(client,
		redistenantstore.WithSecret("test-secret"),
		redistenantstore.WithAvailableTopics(testutil.TestTopics),
		redistenantstore.WithDeploymentID(h.deploymentID),
		redistenantstore.WithMaxDestinationsPerTenant(maxDest),
	)
	if err := s.Init(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (h *redisTenantStoreHarness) MakeIsolatedDrivers(ctx context.Context) (driver.TenantStore, driver.TenantStore, error) {
	client := h.factory(h.t)
	s1 := redistenantstore.New(client,
		redistenantstore.WithSecret("test-secret"),
		redistenantstore.WithAvailableTopics(testutil.TestTopics),
		redistenantstore.WithDeploymentID("dp_001"),
	)
	s2 := redistenantstore.New(client,
		redistenantstore.WithSecret("test-secret"),
		redistenantstore.WithAvailableTopics(testutil.TestTopics),
		redistenantstore.WithDeploymentID("dp_002"),
	)
	if err := s1.Init(ctx); err != nil {
		return nil, nil, err
	}
	if err := s2.Init(ctx); err != nil {
		return nil, nil, err
	}
	return s1, s2, nil
}

func (h *redisTenantStoreHarness) Close() {}

func newHarness(factory redisClientFactory, deploymentID string) drivertest.HarnessMaker {
	return func(_ context.Context, t *testing.T) (drivertest.Harness, error) {
		return &redisTenantStoreHarness{
			factory:      factory,
			t:            t,
			deploymentID: deploymentID,
		}, nil
	}
}

// =============================================================================
// Conformance Tests with miniredis
// =============================================================================

func TestMiniredis(t *testing.T) {
	t.Parallel()
	drivertest.RunConformanceTests(t, newHarness(miniredisFactory, ""))
}

func TestMiniredis_WithDeploymentID(t *testing.T) {
	t.Parallel()
	drivertest.RunConformanceTests(t, newHarness(miniredisFactory, "dp_test_001"))
}

// =============================================================================
// Conformance Tests with Redis Stack
// =============================================================================

func TestRedisStack(t *testing.T) {
	t.Parallel()
	drivertest.RunConformanceTests(t, newHarness(redisStackFactory, ""))
}

func TestRedisStack_WithDeploymentID(t *testing.T) {
	t.Parallel()
	drivertest.RunConformanceTests(t, newHarness(redisStackFactory, "dp_test_001"))
}

// =============================================================================
// Conformance Tests with Dragonfly
// =============================================================================

func TestDragonfly(t *testing.T) {
	t.Parallel()
	drivertest.RunConformanceTests(t, newHarness(dragonflyFactory, ""))
}

func TestDragonfly_WithDeploymentID(t *testing.T) {
	t.Parallel()
	drivertest.RunConformanceTests(t, newHarness(dragonflyFactory, "dp_test_001"))
}

// =============================================================================
// Standalone: Credentials Encryption
// =============================================================================

func TestDestinationCredentialsEncryption(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)
	secret := "test-secret"

	store := redistenantstore.New(redisClient,
		redistenantstore.WithSecret(secret),
		redistenantstore.WithAvailableTopics(testutil.TestTopics),
	)

	input := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("rabbitmq"),
		testutil.DestinationFactory.WithTopics([]string{"user.created", "user.updated"}),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"server_url": "localhost:5672",
			"exchange":   "events",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": "guest",
			"password": "guest",
		}),
		testutil.DestinationFactory.WithDeliveryMetadata(map[string]string{
			"Authorization": "Bearer secret-token",
			"X-API-Key":     "sensitive-key",
		}),
	)

	err := store.UpsertDestination(ctx, input)
	require.NoError(t, err)

	// Access Redis directly to verify encryption (implementation detail)
	keyFormat := "tenant:{%s}:destination:%s"
	actual, err := redisClient.HGetAll(ctx, fmt.Sprintf(keyFormat, input.TenantID, input.ID)).Result()
	require.NoError(t, err)

	// Verify credentials are encrypted (not plaintext JSON)
	jsonCredentials, _ := json.Marshal(input.Credentials)
	assert.NotEqual(t, string(jsonCredentials), actual["credentials"])

	// Verify delivery_metadata is encrypted (not plaintext JSON)
	jsonDeliveryMetadata, _ := json.Marshal(input.DeliveryMetadata)
	assert.NotEqual(t, string(jsonDeliveryMetadata), actual["delivery_metadata"])

	// Verify round-trip: retrieve destination and check values match
	retrieved, err := store.RetrieveDestination(ctx, input.TenantID, input.ID)
	require.NoError(t, err)
	assert.Equal(t, input.Credentials, retrieved.Credentials)
	assert.Equal(t, input.DeliveryMetadata, retrieved.DeliveryMetadata)
}

// =============================================================================
// Standalone: ListTenant Pagination Suite (requires RediSearch)
// =============================================================================

func TestListTenantPagination(t *testing.T) {
	t.Parallel()
	runListTenantPaginationSuite(t, dragonflyStackFactory, "")
}

func TestListTenantPagination_WithDeploymentID(t *testing.T) {
	t.Parallel()
	runListTenantPaginationSuite(t, dragonflyStackFactory, "dp_pagination_test")
}

func TestListTenantPagination_Compat_RedisStack(t *testing.T) {
	t.Parallel()
	runListTenantPaginationSuite(t, redisStackFactory, "")
}

func TestListTenantPagination_Compat_RedisStack_WithDeploymentID(t *testing.T) {
	t.Parallel()
	runListTenantPaginationSuite(t, redisStackFactory, "dp_pagination_test")
}

func runListTenantPaginationSuite(t *testing.T, factory redisClientFactory, deploymentID string) {
	ctx := context.Background()
	redisClient := factory(t)

	// Add unique suffix to isolate parallel runs
	if deploymentID != "" {
		deploymentID = fmt.Sprintf("%s_%d", deploymentID, time.Now().UnixNano())
	} else {
		deploymentID = fmt.Sprintf("pagination_test_%d", time.Now().UnixNano())
	}

	store := redistenantstore.New(redisClient,
		redistenantstore.WithSecret("test-secret"),
		redistenantstore.WithAvailableTopics(testutil.TestTopics),
		redistenantstore.WithDeploymentID(deploymentID),
	)

	err := store.Init(ctx)
	require.NoError(t, err)

	var createdTenantIDs []string
	baseTime := time.Now()

	paginationSuite := paginationtest.Suite[models.Tenant]{
		Name: "redistenantstore_ListTenant",

		NewItem: func(index int) models.Tenant {
			return models.Tenant{
				ID:        fmt.Sprintf("tenant_pagination_%d_%d", time.Now().UnixNano(), index),
				CreatedAt: baseTime.Add(time.Duration(index) * time.Second),
				UpdatedAt: baseTime.Add(time.Duration(index) * time.Second),
			}
		},

		InsertMany: func(ctx context.Context, items []models.Tenant) error {
			for _, item := range items {
				if err := store.UpsertTenant(ctx, item); err != nil {
					return err
				}
				createdTenantIDs = append(createdTenantIDs, item.ID)
			}
			return nil
		},

		List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[models.Tenant], error) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Limit: opts.Limit,
				Dir:   opts.Order,
				Next:  opts.Next,
				Prev:  opts.Prev,
			})
			if err != nil {
				return paginationtest.ListResult[models.Tenant]{}, err
			}
			var next, prev string
			if resp.Pagination.Next != nil {
				next = *resp.Pagination.Next
			}
			if resp.Pagination.Prev != nil {
				prev = *resp.Pagination.Prev
			}
			return paginationtest.ListResult[models.Tenant]{
				Items: resp.Models,
				Next:  next,
				Prev:  prev,
			}, nil
		},

		GetID: func(t models.Tenant) string {
			return t.ID
		},

		Cleanup: func(ctx context.Context) error {
			for _, id := range createdTenantIDs {
				_ = store.DeleteTenant(ctx, id)
			}
			createdTenantIDs = nil
			return nil
		},
	}

	paginationSuite.Run(t)
}

// =============================================================================
// Standalone: ListTenant not supported (miniredis has no RediSearch)
// =============================================================================

func TestListTenantNotSupported(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	store := redistenantstore.New(redisClient,
		redistenantstore.WithSecret("test-secret"),
	)
	require.NoError(t, store.Init(ctx))

	_, err := store.ListTenant(ctx, driver.ListTenantRequest{})
	require.ErrorIs(t, err, driver.ErrListTenantNotSupported)
}

// =============================================================================
// Standalone: Deployment Isolation (same Redis, different deployment IDs)
// =============================================================================

func TestDeploymentIsolation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	store1 := redistenantstore.New(redisClient,
		redistenantstore.WithSecret("test-secret"),
		redistenantstore.WithAvailableTopics(testutil.TestTopics),
		redistenantstore.WithDeploymentID("dp_001"),
	)
	store2 := redistenantstore.New(redisClient,
		redistenantstore.WithSecret("test-secret"),
		redistenantstore.WithAvailableTopics(testutil.TestTopics),
		redistenantstore.WithDeploymentID("dp_002"),
	)

	tenantID := idgen.String()
	destinationID := idgen.Destination()

	tenant := models.Tenant{
		ID:        tenantID,
		CreatedAt: time.Now(),
	}
	require.NoError(t, store1.UpsertTenant(ctx, tenant))
	require.NoError(t, store2.UpsertTenant(ctx, tenant))

	destination1 := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID(destinationID),
		testutil.DestinationFactory.WithTenantID(tenantID),
		testutil.DestinationFactory.WithConfig(map[string]string{"deployment": "dp_001"}),
	)
	destination2 := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID(destinationID),
		testutil.DestinationFactory.WithTenantID(tenantID),
		testutil.DestinationFactory.WithConfig(map[string]string{"deployment": "dp_002"}),
	)

	require.NoError(t, store1.CreateDestination(ctx, destination1))
	require.NoError(t, store2.CreateDestination(ctx, destination2))

	retrieved1, err := store1.RetrieveDestination(ctx, tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, "dp_001", retrieved1.Config["deployment"])

	retrieved2, err := store2.RetrieveDestination(ctx, tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, "dp_002", retrieved2.Config["deployment"])

	// Delete from store1 should not affect store2
	require.NoError(t, store1.DeleteDestination(ctx, tenantID, destinationID))

	_, err = store1.RetrieveDestination(ctx, tenantID, destinationID)
	require.ErrorIs(t, err, driver.ErrDestinationDeleted)

	retrieved2Again, err := store2.RetrieveDestination(ctx, tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, "dp_002", retrieved2Again.Config["deployment"])
}
