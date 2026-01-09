package models_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// miniredisClientFactory creates a miniredis client (in-memory, no RediSearch)
func miniredisClientFactory(t *testing.T) redis.Cmdable {
	return testutil.CreateTestRedisClient(t)
}

// redisStackClientFactory creates a Redis Stack client on DB 0 (RediSearch works)
// Tests using this are serialized since RediSearch only works on DB 0.
func redisStackClientFactory(t *testing.T) redis.Cmdable {
	testinfra.Start(t)
	redisCfg := testinfra.NewRedisStackConfig(t)
	client, err := redis.New(context.Background(), redisCfg)
	if err != nil {
		t.Fatalf("failed to create redis client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// dragonflyClientFactory creates a Dragonfly client (DB 1-15, no RediSearch).
// Tests can run in parallel since each gets its own DB.
func dragonflyClientFactory(t *testing.T) redis.Cmdable {
	testinfra.Start(t)
	redisCfg := testinfra.NewDragonflyConfig(t)
	client, err := redis.New(context.Background(), redisCfg)
	if err != nil {
		t.Fatalf("failed to create dragonfly client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// dragonflyStackClientFactory creates a Dragonfly client on DB 0 (RediSearch works).
// Tests using this are serialized since RediSearch only works on DB 0.
func dragonflyStackClientFactory(t *testing.T) redis.Cmdable {
	testinfra.Start(t)
	redisCfg := testinfra.NewDragonflyStackConfig(t)
	client, err := redis.New(context.Background(), redisCfg)
	if err != nil {
		t.Fatalf("failed to create dragonfly stack client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// =============================================================================
// EntityTestSuite with miniredis (in-memory, no RediSearch)
// =============================================================================

func TestEntityStore_Miniredis_WithoutDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{
		RedisClientFactory: miniredisClientFactory,
		deploymentID:       "",
	})
}

func TestEntityStore_Miniredis_WithDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{
		RedisClientFactory: miniredisClientFactory,
		deploymentID:       "dp_test_001",
	})
}

// =============================================================================
// EntityTestSuite with Redis Stack (real Redis with RediSearch)
// =============================================================================

func TestEntityStore_RedisStack_WithoutDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{
		RedisClientFactory: redisStackClientFactory,
		deploymentID:       "",
	})
}

func TestEntityStore_RedisStack_WithDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{
		RedisClientFactory: redisStackClientFactory,
		deploymentID:       "dp_test_001",
	})
}

// =============================================================================
// EntityTestSuite with Dragonfly (DB 1-15, no RediSearch, faster parallel tests)
// =============================================================================

func TestEntityStore_Dragonfly_WithoutDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{
		RedisClientFactory: dragonflyClientFactory,
		deploymentID:       "",
	})
}

func TestEntityStore_Dragonfly_WithDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{
		RedisClientFactory: dragonflyClientFactory,
		deploymentID:       "dp_test_001",
	})
}

// =============================================================================
// ListTenantTestSuite - only runs with Redis Stack (requires RediSearch)
// =============================================================================

func TestListTenant_RedisStack_WithoutDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &ListTenantTestSuite{
		RedisClientFactory: redisStackClientFactory,
		deploymentID:       "",
	})
}

func TestListTenant_RedisStack_WithDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &ListTenantTestSuite{
		RedisClientFactory: redisStackClientFactory,
		deploymentID:       "dp_test_001",
	})
}

// =============================================================================
// ListTenantTestSuite with Dragonfly Stack (DB 0 for RediSearch)
// =============================================================================

func TestListTenant_Dragonfly_WithoutDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &ListTenantTestSuite{
		RedisClientFactory: dragonflyStackClientFactory,
		deploymentID:       "",
	})
}

func TestListTenant_Dragonfly_WithDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &ListTenantTestSuite{
		RedisClientFactory: dragonflyStackClientFactory,
		deploymentID:       "dp_test_001",
	})
}

// TestDestinationCredentialsEncryption verifies that credentials and delivery_metadata
// are properly encrypted when stored in Redis.
//
// NOTE: This test accesses Redis implementation details directly to verify encryption.
// While this couples the test to the storage implementation, it's necessary to confirm
// that sensitive fields are actually encrypted at rest.
func TestDestinationCredentialsEncryption(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)
	cipher := models.NewAESCipher("secret")

	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(cipher),
		models.WithAvailableTopics(testutil.TestTopics),
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

	err := entityStore.UpsertDestination(ctx, input)
	require.NoError(t, err)

	// Access Redis directly to verify encryption (implementation detail)
	keyFormat := "tenant:{%s}:destination:%s"
	actual, err := redisClient.HGetAll(ctx, fmt.Sprintf(keyFormat, input.TenantID, input.ID)).Result()
	require.NoError(t, err)

	// Verify credentials are encrypted (not plaintext)
	assert.NotEqual(t, input.Credentials, actual["credentials"])

	// Verify we can decrypt credentials back to original
	decryptedCredentials, err := cipher.Decrypt([]byte(actual["credentials"]))
	require.NoError(t, err)
	jsonCredentials, _ := json.Marshal(input.Credentials)
	assert.Equal(t, string(jsonCredentials), string(decryptedCredentials))

	// Verify delivery_metadata is encrypted (not plaintext)
	assert.NotEqual(t, input.DeliveryMetadata, actual["delivery_metadata"])

	// Verify we can decrypt delivery_metadata back to original
	decryptedDeliveryMetadata, err := cipher.Decrypt([]byte(actual["delivery_metadata"]))
	require.NoError(t, err)
	jsonDeliveryMetadata, _ := json.Marshal(input.DeliveryMetadata)
	assert.Equal(t, string(jsonDeliveryMetadata), string(decryptedDeliveryMetadata))
}

// TestMaxDestinationsPerTenant verifies that the entity store properly enforces
// the maximum destinations per tenant limit.
func TestMaxDestinationsPerTenant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)
	maxDestinations := 2

	limitedStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
		models.WithMaxDestinationsPerTenant(maxDestinations),
	)

	tenant := models.Tenant{
		ID:        idgen.String(),
		CreatedAt: time.Now(),
	}
	require.NoError(t, limitedStore.UpsertTenant(ctx, tenant))

	// Should be able to create up to maxDestinations
	for i := 0; i < maxDestinations; i++ {
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenant.ID),
		)
		err := limitedStore.CreateDestination(ctx, destination)
		require.NoError(t, err, "Should be able to create destination %d", i+1)
	}

	// Should fail when trying to create one more
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	err := limitedStore.CreateDestination(ctx, destination)
	require.Error(t, err)
	require.ErrorIs(t, err, models.ErrMaxDestinationsPerTenantReached)

	// Should be able to create after deleting one
	destinations, err := limitedStore.ListDestinationByTenant(ctx, tenant.ID)
	require.NoError(t, err)
	require.NoError(t, limitedStore.DeleteDestination(ctx, tenant.ID, destinations[0].ID))

	destination = testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	err = limitedStore.CreateDestination(ctx, destination)
	require.NoError(t, err, "Should be able to create destination after deleting one")
}

// TestDeploymentIsolation verifies that entity stores with different deployment IDs
// are completely isolated from each other, even when sharing the same Redis instance.
func TestDeploymentIsolation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)

	// Create two entity stores with different deployment IDs
	store1 := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
		models.WithDeploymentID("dp_001"),
	)

	store2 := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
		models.WithDeploymentID("dp_002"),
	)

	// Use the SAME tenant ID and destination ID for both deployments
	tenantID := idgen.String()
	destinationID := idgen.Destination()

	// Create tenant in both deployments
	tenant := models.Tenant{
		ID:        tenantID,
		CreatedAt: time.Now(),
	}
	require.NoError(t, store1.UpsertTenant(ctx, tenant))
	require.NoError(t, store2.UpsertTenant(ctx, tenant))

	// Create destination with same ID but different config in each deployment
	destination1 := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID(destinationID),
		testutil.DestinationFactory.WithTenantID(tenantID),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"deployment": "dp_001",
		}),
	)
	destination2 := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID(destinationID),
		testutil.DestinationFactory.WithTenantID(tenantID),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"deployment": "dp_002",
		}),
	)

	require.NoError(t, store1.CreateDestination(ctx, destination1))
	require.NoError(t, store2.CreateDestination(ctx, destination2))

	// Verify store1 only sees its own data
	retrieved1, err := store1.RetrieveDestination(ctx, tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, "dp_001", retrieved1.Config["deployment"], "Store 1 should see its own data")

	// Verify store2 only sees its own data
	retrieved2, err := store2.RetrieveDestination(ctx, tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, "dp_002", retrieved2.Config["deployment"], "Store 2 should see its own data")

	// Verify list operations are also isolated
	list1, err := store1.ListDestinationByTenant(ctx, tenantID)
	require.NoError(t, err)
	require.Len(t, list1, 1, "Store 1 should only see 1 destination")
	assert.Equal(t, "dp_001", list1[0].Config["deployment"])

	list2, err := store2.ListDestinationByTenant(ctx, tenantID)
	require.NoError(t, err)
	require.Len(t, list2, 1, "Store 2 should only see 1 destination")
	assert.Equal(t, "dp_002", list2[0].Config["deployment"])

	// Verify deleting from one deployment doesn't affect the other
	require.NoError(t, store1.DeleteDestination(ctx, tenantID, destinationID))

	// Store1 should not find the destination
	_, err = store1.RetrieveDestination(ctx, tenantID, destinationID)
	require.ErrorIs(t, err, models.ErrDestinationDeleted)

	// Store2 should still have its destination
	retrieved2Again, err := store2.RetrieveDestination(ctx, tenantID, destinationID)
	require.NoError(t, err)
	assert.Equal(t, "dp_002", retrieved2Again.Config["deployment"], "Store 2 data should be unaffected")
}
