package models_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

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
