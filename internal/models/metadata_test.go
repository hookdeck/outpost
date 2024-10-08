package models_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadataRepo_TenantCRUD(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	metadataRepo := models.NewMetadataRepo(redisClient, models.NewAESCipher("secret"))

	input := models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}

	t.Run("gets empty", func(t *testing.T) {
		actual, err := metadataRepo.RetrieveTenant(context.Background(), input.ID)
		assert.Nil(t, actual)
		assert.Nil(t, err)
	})

	t.Run("sets", func(t *testing.T) {
		err := metadataRepo.UpsertTenant(context.Background(), input)
		require.Nil(t, err)

		hash, err := redisClient.HGetAll(context.Background(), "tenant:"+input.ID).Result()
		require.Nil(t, err)
		createdAt, err := time.Parse(time.RFC3339Nano, hash["created_at"])
		require.Nil(t, err)
		assert.True(t, input.CreatedAt.Equal(createdAt))
	})

	t.Run("gets", func(t *testing.T) {
		actual, err := metadataRepo.RetrieveTenant(context.Background(), input.ID)
		require.Nil(t, err)
		assert.True(t, cmp.Equal(input, *actual))
	})

	t.Run("overrides", func(t *testing.T) {
		input.CreatedAt = time.Now()

		err := metadataRepo.UpsertTenant(context.Background(), input)
		require.Nil(t, err)

		actual, err := metadataRepo.RetrieveTenant(context.Background(), input.ID)
		require.Nil(t, err)
		assert.True(t, cmp.Equal(input, *actual))
	})

	t.Run("clears", func(t *testing.T) {
		err := metadataRepo.DeleteTenant(context.Background(), input.ID)
		require.Nil(t, err)

		actual, err := metadataRepo.RetrieveTenant(context.Background(), input.ID)
		require.Nil(t, err)
		assert.Nil(t, actual)
	})
}

func TestMetadataRepo_DestinationCRUD(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	metadataRepo := models.NewMetadataRepo(redisClient, models.NewAESCipher("secret"))

	input := models.Destination{
		ID:     uuid.New().String(),
		Type:   "rabbitmq",
		Topics: []string{"user.created", "user.updated"},
		Config: map[string]string{
			"server_url": "localhost:5672",
			"exchange":   "events",
		},
		Credentials: map[string]string{
			"username": "guest",
			"password": "guest",
		},
		CreatedAt:  time.Now(),
		DisabledAt: nil,
		TenantID:   uuid.New().String(),
	}

	t.Run("gets empty", func(t *testing.T) {
		actual, err := metadataRepo.RetrieveDestination(context.Background(), input.TenantID, input.ID)
		require.Nil(t, err)
		assert.Nil(t, actual)
	})

	t.Run("sets", func(t *testing.T) {
		err := metadataRepo.UpsertDestination(context.Background(), input)
		require.Nil(t, err)
	})

	t.Run("gets", func(t *testing.T) {
		actual, err := metadataRepo.RetrieveDestination(context.Background(), input.TenantID, input.ID)
		require.Nil(t, err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("overrides", func(t *testing.T) {
		input.Topics = []string{"*"}

		err := metadataRepo.UpsertDestination(context.Background(), input)
		require.Nil(t, err)

		actual, err := metadataRepo.RetrieveDestination(context.Background(), input.TenantID, input.ID)
		require.Nil(t, err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("clears", func(t *testing.T) {
		err := metadataRepo.DeleteDestination(context.Background(), input.TenantID, input.ID)
		require.Nil(t, err)

		actual, err := metadataRepo.RetrieveDestination(context.Background(), input.TenantID, input.ID)
		require.Nil(t, err)
		assert.Nil(t, actual)
	})
}

func TestMetadataRepo_ListDestinationEmpty(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	metadataRepo := models.NewMetadataRepo(redisClient, models.NewAESCipher("secret"))

	destinations, err := metadataRepo.ListDestinationByTenant(context.Background(), uuid.New().String())
	require.Nil(t, err)
	assert.Empty(t, destinations)
}

func TestMetadataRepo_DeleteTenantAndAssociatedDestinations(t *testing.T) {
	t.Parallel()
	redisClient := testutil.CreateTestRedisClient(t)
	metadataRepo := models.NewMetadataRepo(redisClient, models.NewAESCipher("secret"))
	tenant := models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
	// Arrange
	require.Nil(t, metadataRepo.UpsertTenant(context.Background(), tenant))
	destinationIDs := []string{uuid.New().String(), uuid.New().String(), uuid.New().String()}
	require.Nil(t, metadataRepo.UpsertDestination(context.Background(), mockDestinationFactory.Any(
		mockDestinationFactory.WithID(destinationIDs[0]),
		mockDestinationFactory.WithTenantID(tenant.ID),
	)))
	require.Nil(t, metadataRepo.UpsertDestination(context.Background(), mockDestinationFactory.Any(
		mockDestinationFactory.WithID(destinationIDs[1]),
		mockDestinationFactory.WithTenantID(tenant.ID),
	)))
	require.Nil(t, metadataRepo.UpsertDestination(context.Background(), mockDestinationFactory.Any(
		mockDestinationFactory.WithID(destinationIDs[2]),
		mockDestinationFactory.WithTenantID(tenant.ID),
	)))
	require.Equal(t, int64(1), redisClient.Exists(context.Background(), "tenant:"+tenant.ID).Val())
	require.Equal(t, int64(1), redisClient.Exists(context.Background(), "tenant:"+tenant.ID+":destination:"+destinationIDs[0]).Val())
	require.Equal(t, int64(1), redisClient.Exists(context.Background(), "tenant:"+tenant.ID+":destination:"+destinationIDs[1]).Val())
	require.Equal(t, int64(1), redisClient.Exists(context.Background(), "tenant:"+tenant.ID+":destination:"+destinationIDs[2]).Val())
	// Act
	require.Nil(t, metadataRepo.DeleteTenant(context.Background(), tenant.ID))
	// Assert
	assert.Equal(t, int64(0), redisClient.Exists(context.Background(), "tenant:"+tenant.ID).Val())
	assert.Equal(t, int64(0), redisClient.Exists(context.Background(), "tenant:"+tenant.ID+":destination:"+destinationIDs[0]).Val())
	assert.Equal(t, int64(0), redisClient.Exists(context.Background(), "tenant:"+tenant.ID+":destination:"+destinationIDs[1]).Val())
	assert.Equal(t, int64(0), redisClient.Exists(context.Background(), "tenant:"+tenant.ID+":destination:"+destinationIDs[2]).Val())
}

func TestMetadataRepo_DestinationCredentialsEncryption(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	cipher := models.NewAESCipher("secret")
	metadataRepo := models.NewMetadataRepo(redisClient, cipher)

	testMetadataRepoDestinationCredentialsEncryption(t, redisClient, cipher, metadataRepo)
}

func testMetadataRepoDestinationCredentialsEncryption(t *testing.T, redisClient *redis.Client, cipher models.Cipher, metadataRepo models.MetadataRepo) {
	input := models.Destination{
		ID:     uuid.New().String(),
		Type:   "rabbitmq",
		Topics: []string{"user.created", "user.updated"},
		Config: map[string]string{
			"server_url": "localhost:5672",
			"exchange":   "events",
		},
		Credentials: map[string]string{
			"username": "guest",
			"password": "guest",
		},
		CreatedAt:  time.Now(),
		DisabledAt: nil,
		TenantID:   uuid.New().String(),
	}

	err := metadataRepo.UpsertDestination(context.Background(), input)
	require.Nil(t, err)

	actual, err := redisClient.HGetAll(context.Background(), fmt.Sprintf("tenant:%s:destination:%s", input.TenantID, input.ID)).Result()
	require.Nil(t, err)
	assert.NotEqual(t, input.Credentials, actual["credentials"])
	decryptedCredentials, err := cipher.Decrypt([]byte(actual["credentials"]))
	require.Nil(t, err)
	jsonCredentials, _ := json.Marshal(input.Credentials)
	assert.Equal(t, string(jsonCredentials), string(decryptedCredentials))
}

func assertEqualDestination(t *testing.T, expected, actual models.Destination) {
	assert.Equal(t, expected.ID, actual.ID)
	assert.Equal(t, expected.Type, actual.Type)
	assert.Equal(t, expected.Topics, actual.Topics)
	assert.Equal(t, expected.Config, actual.Config)
	assert.Equal(t, expected.Credentials, actual.Credentials)
	assert.True(t, cmp.Equal(expected.CreatedAt, actual.CreatedAt))
	assert.True(t, cmp.Equal(expected.DisabledAt, actual.DisabledAt))
}

func TestMetadataRepo_MatchEvent(t *testing.T) {
	t.Parallel()

	// Arrange
	var err error
	redisClient := testutil.CreateTestRedisClient(t)
	metadataRepo := models.NewMetadataRepo(redisClient, models.NewAESCipher("secret"))

	tenant := models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}

	err = metadataRepo.UpsertTenant(context.Background(), tenant)
	require.Nil(t, err)

	ids := []string{
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
	}

	err = metadataRepo.UpsertDestination(context.Background(),
		mockDestinationFactory.Any(
			mockDestinationFactory.WithID(ids[0]),
			mockDestinationFactory.WithTenantID(tenant.ID),
			mockDestinationFactory.WithTopics([]string{"*"}),
		),
	)
	require.Nil(t, err)
	err = metadataRepo.UpsertDestination(context.Background(),
		mockDestinationFactory.Any(
			mockDestinationFactory.WithID(ids[1]),
			mockDestinationFactory.WithTenantID(tenant.ID),
			mockDestinationFactory.WithTopics([]string{"user.created", "user.updated"}),
		),
	)
	require.Nil(t, err)
	err = metadataRepo.UpsertDestination(context.Background(),
		mockDestinationFactory.Any(
			mockDestinationFactory.WithID(ids[2]),
			mockDestinationFactory.WithTenantID(tenant.ID),
			mockDestinationFactory.WithTopics([]string{"user.created"}),
		),
	)
	require.Nil(t, err)
	err = metadataRepo.UpsertDestination(context.Background(),
		mockDestinationFactory.Any(
			mockDestinationFactory.WithID(ids[3]),
			mockDestinationFactory.WithTenantID(tenant.ID),
			mockDestinationFactory.WithTopics([]string{"user.updated"}),
		),
	)
	require.Nil(t, err)

	// Delete destination to test if destination is cleaned up properly
	err = metadataRepo.UpsertDestination(context.Background(),
		mockDestinationFactory.Any(
			mockDestinationFactory.WithID(ids[4]),
			mockDestinationFactory.WithTenantID(tenant.ID),
			mockDestinationFactory.WithTopics([]string{"*"}),
		),
	)
	require.Nil(t, err)
	err = metadataRepo.DeleteDestination(context.Background(), tenant.ID, ids[4])
	require.Nil(t, err)

	// Act
	event := models.Event{
		ID:       uuid.New().String(),
		Topic:    "user.created",
		Time:     time.Now(),
		TenantID: tenant.ID,
		Metadata: map[string]string{},
		Data:     map[string]interface{}{},
	}
	matchedDestinationSummaryList, err := metadataRepo.MatchEvent(context.Background(), event)
	require.Nil(t, err)

	// Assert
	require.Len(t, matchedDestinationSummaryList, 3)
	for _, summary := range matchedDestinationSummaryList {
		require.Contains(t, []string{ids[0], ids[1], ids[2]}, summary.ID)
	}
}

type multiDestinationSuite struct {
	ctx          context.Context
	redisClient  *redis.Client
	metadataRepo models.MetadataRepo
	tenant       models.Tenant
	destinations []models.Destination
}

func (suite *multiDestinationSuite) SetupTest(t *testing.T) {
	suite.ctx = context.Background()
	suite.redisClient = testutil.CreateTestRedisClient(t)
	suite.metadataRepo = models.NewMetadataRepo(suite.redisClient, models.NewAESCipher("secret"))
	suite.destinations = make([]models.Destination, 5)
	suite.tenant = models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
	err := suite.metadataRepo.UpsertTenant(suite.ctx, suite.tenant)
	require.Nil(t, err)

	ids := make([]string, 5)
	for i := 0; i < 5; i++ {
		ids[i] = uuid.New().String()
		suite.destinations[i] = mockDestinationFactory.Any(
			mockDestinationFactory.WithID(ids[i]),
			mockDestinationFactory.WithTenantID(suite.tenant.ID),
		)
		err = suite.metadataRepo.UpsertDestination(suite.ctx, suite.destinations[i])
		require.Nil(t, err)
	}
}

func TestMultiDestinationSuite_ListDestinationByTenant(t *testing.T) {
	t.Parallel()
	suite := multiDestinationSuite{}
	suite.SetupTest(t)

	destinations, err := suite.metadataRepo.ListDestinationByTenant(suite.ctx, suite.tenant.ID)
	require.Nil(t, err)
	require.Len(t, destinations, 5)
	for index, destination := range destinations {
		require.Equal(t, suite.destinations[index].ID, destination.ID)
	}
}

func TestMultiDestinationSuite_UpdateDestination(t *testing.T) {
	t.Parallel()
	suite := multiDestinationSuite{}
	suite.SetupTest(t)

	updatedIndex := 2
	updatedTopics := []string{"user.created"}
	updatedDestination := suite.destinations[updatedIndex]
	updatedDestination.Topics = updatedTopics
	err := suite.metadataRepo.UpsertDestination(suite.ctx, updatedDestination)
	require.Nil(t, err)

	actual, err := suite.metadataRepo.RetrieveDestination(suite.ctx, updatedDestination.TenantID, updatedDestination.ID)
	require.Nil(t, err)
	assert.Equal(t, updatedDestination.Topics, actual.Topics)

	destinations, err := suite.metadataRepo.ListDestinationByTenant(suite.ctx, suite.tenant.ID)
	require.Nil(t, err)
	assert.Len(t, destinations, 5)

	destinationSummaryList, err := suite.metadataRepo.ListDestinationSummaryByTenant(suite.ctx, suite.tenant.ID)
	require.Nil(t, err)
	require.Len(t, destinationSummaryList, 5)
	foundMatchingDestination := false
	for _, destinationSummary := range destinationSummaryList {
		if destinationSummary.ID == updatedDestination.ID {
			foundMatchingDestination = true
			assert.Equal(t, updatedDestination.Topics, destinationSummary.Topics)
		}
	}
	assert.True(t, foundMatchingDestination, "Unable to find destination in destination summary list")
}
