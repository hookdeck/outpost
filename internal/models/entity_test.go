package models_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestEntityStore_WithoutDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{deploymentID: ""})
}

func TestEntityStore_WithDeploymentID(t *testing.T) {
	t.Parallel()
	suite.Run(t, &EntityTestSuite{deploymentID: "dp_test_001"})
}

// TestDestinationCredentialsEncryption verifies that credentials are properly encrypted
// when stored in Redis.
//
// NOTE: This test accesses Redis implementation details directly to verify encryption.
// While this couples the test to the storage implementation, it's necessary to confirm
// that credentials are actually encrypted at rest.
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
	)

	err := entityStore.UpsertDestination(ctx, input)
	require.NoError(t, err)

	// Access Redis directly to verify encryption (implementation detail)
	keyFormat := "tenant:{%s}:destination:%s"
	actual, err := redisClient.HGetAll(ctx, fmt.Sprintf(keyFormat, input.TenantID, input.ID)).Result()
	require.NoError(t, err)

	// Verify credentials are encrypted (not plaintext)
	assert.NotEqual(t, input.Credentials, actual["credentials"])

	// Verify we can decrypt back to original
	decryptedCredentials, err := cipher.Decrypt([]byte(actual["credentials"]))
	require.NoError(t, err)
	jsonCredentials, _ := json.Marshal(input.Credentials)
	assert.Equal(t, string(jsonCredentials), string(decryptedCredentials))
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
		ID:        uuid.New().String(),
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
	tenantID := uuid.New().String()
	destinationID := uuid.New().String()

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
