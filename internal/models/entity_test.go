package models_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntityStore_TenantCRUD(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
	)

	input := models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}

	t.Run("gets empty", func(t *testing.T) {
		actual, err := entityStore.RetrieveTenant(context.Background(), input.ID)
		assert.Nil(t, actual)
		assert.NoError(t, err)
	})

	t.Run("sets", func(t *testing.T) {
		err := entityStore.UpsertTenant(context.Background(), input)
		require.NoError(t, err)

		hash, err := redisClient.HGetAll(context.Background(), "tenant:"+input.ID).Result()
		require.NoError(t, err)
		createdAt, err := time.Parse(time.RFC3339Nano, hash["created_at"])
		require.NoError(t, err)
		assert.True(t, input.CreatedAt.Equal(createdAt))
	})

	t.Run("gets", func(t *testing.T) {
		actual, err := entityStore.RetrieveTenant(context.Background(), input.ID)
		require.NoError(t, err)
		assert.Equal(t, input.ID, actual.ID)
		assert.True(t, input.CreatedAt.Equal(actual.CreatedAt))
	})

	t.Run("overrides", func(t *testing.T) {
		input.CreatedAt = time.Now()

		err := entityStore.UpsertTenant(context.Background(), input)
		require.NoError(t, err)

		actual, err := entityStore.RetrieveTenant(context.Background(), input.ID)
		require.NoError(t, err)
		assert.Equal(t, input.ID, actual.ID)
		assert.True(t, input.CreatedAt.Equal(actual.CreatedAt))
	})

	t.Run("clears", func(t *testing.T) {
		require.NoError(t, entityStore.DeleteTenant(context.Background(), input.ID))

		actual, err := entityStore.RetrieveTenant(context.Background(), input.ID)
		assert.ErrorIs(t, err, models.ErrTenantDeleted)
		assert.Nil(t, actual)
	})

	t.Run("deletes again", func(t *testing.T) {
		assert.NoError(t, entityStore.DeleteTenant(context.Background(), input.ID))
	})

	t.Run("deletes non-existent", func(t *testing.T) {
		assert.ErrorIs(t, entityStore.DeleteTenant(context.Background(), "non-existent-tenant"), models.ErrTenantNotFound)
	})

	t.Run("creates & overrides deleted resource", func(t *testing.T) {
		require.NoError(t, entityStore.UpsertTenant(context.Background(), input))

		actual, err := entityStore.RetrieveTenant(context.Background(), input.ID)
		require.NoError(t, err)
		assert.Equal(t, input.ID, actual.ID)
		assert.True(t, input.CreatedAt.Equal(actual.CreatedAt))
	})
}

func TestEntityStore_DestinationCRUD(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
	)

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
		actual, err := entityStore.RetrieveDestination(context.Background(), input.TenantID, input.ID)
		require.NoError(t, err)
		assert.Nil(t, actual)
	})

	t.Run("sets", func(t *testing.T) {
		err := entityStore.CreateDestination(context.Background(), input)
		require.NoError(t, err)
	})

	t.Run("gets", func(t *testing.T) {
		actual, err := entityStore.RetrieveDestination(context.Background(), input.TenantID, input.ID)
		require.NoError(t, err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("updates", func(t *testing.T) {
		input.Topics = []string{"*"}

		err := entityStore.UpsertDestination(context.Background(), input)
		require.NoError(t, err)

		actual, err := entityStore.RetrieveDestination(context.Background(), input.TenantID, input.ID)
		require.NoError(t, err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("clears", func(t *testing.T) {
		err := entityStore.DeleteDestination(context.Background(), input.TenantID, input.ID)
		require.NoError(t, err)

		actual, err := entityStore.RetrieveDestination(context.Background(), input.TenantID, input.ID)
		assert.ErrorIs(t, err, models.ErrDestinationDeleted)
		assert.Nil(t, actual)
	})

	t.Run("creates & overrides deleted resource", func(t *testing.T) {
		err := entityStore.CreateDestination(context.Background(), input)
		require.NoError(t, err)

		actual, err := entityStore.RetrieveDestination(context.Background(), input.TenantID, input.ID)
		require.NoError(t, err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("err when creates duplicate", func(t *testing.T) {
		assert.ErrorIs(t, entityStore.CreateDestination(context.Background(), input), models.ErrDuplicateDestination)

		// cleanup
		require.NoError(t, entityStore.DeleteDestination(context.Background(), input.TenantID, input.ID))
	})
}

func TestEntityStore_ListDestinationEmpty(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
	)

	destinations, err := entityStore.ListDestinationByTenant(context.Background(), uuid.New().String())
	require.NoError(t, err)
	assert.Empty(t, destinations)
}

func TestEntityStore_DeleteTenantAndAssociatedDestinations(t *testing.T) {
	t.Parallel()
	redisClient := testutil.CreateTestRedisClient(t)
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
	)
	tenant := models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
	// Arrange
	require.NoError(t, entityStore.UpsertTenant(context.Background(), tenant))
	destinationIDs := []string{uuid.New().String(), uuid.New().String(), uuid.New().String()}
	for _, id := range destinationIDs {
		require.NoError(t, entityStore.UpsertDestination(context.Background(), testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(id),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
		)))
	}
	// Act
	require.NoError(t, entityStore.DeleteTenant(context.Background(), tenant.ID))
	// Assert
	_, err := entityStore.RetrieveTenant(context.Background(), tenant.ID)
	assert.ErrorIs(t, err, models.ErrTenantDeleted)
	for _, id := range destinationIDs {
		_, err := entityStore.RetrieveDestination(context.Background(), tenant.ID, id)
		assert.ErrorIs(t, err, models.ErrDestinationDeleted)
	}
}

func TestEntityStore_DestinationCredentialsEncryption(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	cipher := models.NewAESCipher("secret")
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(cipher),
		models.WithAvailableTopics(testutil.TestTopics),
	)

	testEntityStoreDestinationCredentialsEncryption(t, redisClient, cipher, entityStore)
}

func testEntityStoreDestinationCredentialsEncryption(t *testing.T, redisClient *redis.Client, cipher models.Cipher, entityStore models.EntityStore) {
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

	err := entityStore.UpsertDestination(context.Background(), input)
	require.NoError(t, err)

	actual, err := redisClient.HGetAll(context.Background(), fmt.Sprintf("tenant:%s:destination:%s", input.TenantID, input.ID)).Result()
	require.NoError(t, err)
	assert.NotEqual(t, input.Credentials, actual["credentials"])
	decryptedCredentials, err := cipher.Decrypt([]byte(actual["credentials"]))
	require.NoError(t, err)
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

type multiDestinationSuite struct {
	ctx          context.Context
	redisClient  *redis.Client
	entityStore  models.EntityStore
	tenant       models.Tenant
	destinations []models.Destination
}

func (suite *multiDestinationSuite) SetupTest(t *testing.T) {
	if suite.ctx == nil {
		suite.ctx = context.Background()
	}
	suite.redisClient = testutil.CreateTestRedisClient(t)
	suite.entityStore = models.NewEntityStore(suite.redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
	)
	suite.destinations = make([]models.Destination, 5)
	suite.tenant = models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
	require.NoError(t, suite.entityStore.UpsertTenant(suite.ctx, suite.tenant))

	ids := make([]string, 5)
	destinationTopicList := [][]string{
		{"*"},
		{"user.created"},
		{"user.updated"},
		{"user.deleted"},
		{"user.created", "user.updated"},
	}
	for i := 0; i < 5; i++ {
		ids[i] = uuid.New().String()
		suite.destinations[i] = testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(ids[i]),
			testutil.DestinationFactory.WithTenantID(suite.tenant.ID),
			testutil.DestinationFactory.WithTopics(destinationTopicList[i]),
		)
		require.NoError(t, suite.entityStore.UpsertDestination(suite.ctx, suite.destinations[i]))
	}

	// Insert & Delete destination to ensure it's cleaned up properly
	toBeDeletedID := uuid.New().String()
	require.NoError(t, suite.entityStore.UpsertDestination(suite.ctx,
		testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(toBeDeletedID),
			testutil.DestinationFactory.WithTenantID(suite.tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
		)))
	require.NoError(t, suite.entityStore.DeleteDestination(suite.ctx, suite.tenant.ID, toBeDeletedID))
}

func TestMultiDestinationSuite_RetrieveTenant_DestinationsCount(t *testing.T) {
	t.Parallel()
	suite := multiDestinationSuite{}
	suite.SetupTest(t)

	tenant, err := suite.entityStore.RetrieveTenant(suite.ctx, suite.tenant.ID)
	require.NoError(t, err)
	require.Equal(t, 5, tenant.DestinationsCount)
}

func TestMultiDestinationSuite_RetrieveTenant_Topics(t *testing.T) {
	t.Parallel()
	suite := multiDestinationSuite{}
	suite.SetupTest(t)

	tenant, err := suite.entityStore.RetrieveTenant(suite.ctx, suite.tenant.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

	require.NoError(t, suite.entityStore.DeleteDestination(suite.ctx, suite.tenant.ID, suite.destinations[0].ID))
	tenant, err = suite.entityStore.RetrieveTenant(suite.ctx, suite.tenant.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

	require.NoError(t, suite.entityStore.DeleteDestination(suite.ctx, suite.tenant.ID, suite.destinations[1].ID))
	tenant, err = suite.entityStore.RetrieveTenant(suite.ctx, suite.tenant.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

	require.NoError(t, suite.entityStore.DeleteDestination(suite.ctx, suite.tenant.ID, suite.destinations[2].ID))
	tenant, err = suite.entityStore.RetrieveTenant(suite.ctx, suite.tenant.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

	require.NoError(t, suite.entityStore.DeleteDestination(suite.ctx, suite.tenant.ID, suite.destinations[3].ID))
	tenant, err = suite.entityStore.RetrieveTenant(suite.ctx, suite.tenant.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"user.created", "user.updated"}, tenant.Topics)

	require.NoError(t, suite.entityStore.DeleteDestination(suite.ctx, suite.tenant.ID, suite.destinations[4].ID))
	tenant, err = suite.entityStore.RetrieveTenant(suite.ctx, suite.tenant.ID)
	require.NoError(t, err)
	require.Equal(t, []string{}, tenant.Topics)
}

func TestMultiDestinationSuite_ListDestinationByTenant(t *testing.T) {
	t.Parallel()
	suite := multiDestinationSuite{}
	suite.SetupTest(t)

	destinations, err := suite.entityStore.ListDestinationByTenant(suite.ctx, suite.tenant.ID)
	require.NoError(t, err)
	require.Len(t, destinations, 5)
	for index, destination := range destinations {
		require.Equal(t, suite.destinations[index].ID, destination.ID)
	}
}

func TestMultiDestinationSuite_ListDestination_WithOpts(t *testing.T) {
	t.Parallel()

	suite := multiDestinationSuite{}
	suite.SetupTest(t)

	t.Run("filter by type: webhook", func(t *testing.T) {
		destinations, err := suite.entityStore.ListDestinationByTenant(suite.ctx, suite.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Type: []string{"webhook"},
		}))
		require.NoError(t, err)
		require.Len(t, destinations, 5)
	})

	t.Run("filter by type: rabbitmq", func(t *testing.T) {
		destinations, err := suite.entityStore.ListDestinationByTenant(suite.ctx, suite.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Type: []string{"rabbitmq"},
		}))
		require.NoError(t, err)
		require.Len(t, destinations, 0)
	})

	t.Run("filter by type: webhook,rabbitmq", func(t *testing.T) {
		destinations, err := suite.entityStore.ListDestinationByTenant(suite.ctx, suite.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Type: []string{"webhook", "rabbitmq"},
		}))
		require.NoError(t, err)
		require.Len(t, destinations, 5)
	})

	t.Run("filter by topic: user.created", func(t *testing.T) {
		destinations, err := suite.entityStore.ListDestinationByTenant(suite.ctx, suite.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Topics: []string{"user.created"},
		}))
		require.NoError(t, err)
		require.Len(t, destinations, 3)
	})

	t.Run("filter by topic: user.created,user.updated", func(t *testing.T) {
		destinations, err := suite.entityStore.ListDestinationByTenant(suite.ctx, suite.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Topics: []string{"user.created", "user.updated"},
		}))
		require.NoError(t, err)
		require.Len(t, destinations, 2)
	})

	t.Run("filter by type: rabbitmq, topic: user.created,user.updated", func(t *testing.T) {
		destinations, err := suite.entityStore.ListDestinationByTenant(suite.ctx, suite.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Type:   []string{"rabbitmq"},
			Topics: []string{"user.created", "user.updated"},
		}))
		require.NoError(t, err)
		require.Len(t, destinations, 0)
	})

	t.Run("filter by topic: *", func(t *testing.T) {
		destinations, err := suite.entityStore.ListDestinationByTenant(suite.ctx, suite.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Topics: []string{"*"},
		}))
		require.NoError(t, err)
		require.Len(t, destinations, 1)
	})
}

func TestMultiDestinationSuite_MatchEvent(t *testing.T) {
	t.Parallel()

	suite := multiDestinationSuite{}
	suite.SetupTest(t)

	t.Run("match by topic", func(t *testing.T) {
		// Act
		event := models.Event{
			ID:       uuid.New().String(),
			Topic:    "user.created",
			Time:     time.Now(),
			TenantID: suite.tenant.ID,
			Metadata: map[string]string{},
			Data:     map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := suite.entityStore.MatchEvent(suite.ctx, event)
		require.NoError(t, err)

		// Assert
		require.Len(t, matchedDestinationSummaryList, 3)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(t, []string{suite.destinations[0].ID, suite.destinations[1].ID, suite.destinations[4].ID}, summary.ID)
		}
	})

	t.Run("match by topic & destination", func(t *testing.T) {
		// Act
		event := models.Event{
			ID:            uuid.New().String(),
			Topic:         "user.created",
			Time:          time.Now(),
			TenantID:      suite.tenant.ID,
			DestinationID: suite.destinations[1].ID,
			Metadata:      map[string]string{},
			Data:          map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := suite.entityStore.MatchEvent(suite.ctx, event)
		require.NoError(t, err)

		// Assert
		require.Len(t, matchedDestinationSummaryList, 1)
		require.Equal(t, suite.destinations[1].ID, matchedDestinationSummaryList[0].ID)
	})

	t.Run("destination not found", func(t *testing.T) {
		// Act
		event := models.Event{
			ID:            uuid.New().String(),
			Topic:         "user.created",
			Time:          time.Now(),
			TenantID:      suite.tenant.ID,
			DestinationID: "not-found",
			Metadata:      map[string]string{},
			Data:          map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := suite.entityStore.MatchEvent(suite.ctx, event)
		require.NoError(t, err)

		// Assert
		require.Len(t, matchedDestinationSummaryList, 0)
	})

	t.Run("destination topic is invalid", func(t *testing.T) {
		// Act
		event := models.Event{
			ID:            uuid.New().String(),
			Topic:         "user.created",
			Time:          time.Now(),
			TenantID:      suite.tenant.ID,
			DestinationID: suite.destinations[3].ID, // "user-deleted" destination
			Metadata:      map[string]string{},
			Data:          map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := suite.entityStore.MatchEvent(suite.ctx, event)
		require.NoError(t, err)

		// Assert
		require.Len(t, matchedDestinationSummaryList, 0)
	})

	t.Run("match after destination is updated", func(t *testing.T) {
		updatedIndex := 2
		updatedTopics := []string{"user.created"}
		updatedDestination := suite.destinations[updatedIndex]
		updatedDestination.Topics = updatedTopics
		require.NoError(t, suite.entityStore.UpsertDestination(suite.ctx, updatedDestination))

		actual, err := suite.entityStore.RetrieveDestination(suite.ctx, updatedDestination.TenantID, updatedDestination.ID)
		require.NoError(t, err)
		assert.Equal(t, updatedDestination.Topics, actual.Topics)

		destinations, err := suite.entityStore.ListDestinationByTenant(suite.ctx, suite.tenant.ID)
		require.NoError(t, err)
		assert.Len(t, destinations, 5)

		// Match user.created
		event := models.Event{
			ID:       uuid.New().String(),
			Topic:    "user.created",
			Time:     time.Now(),
			TenantID: suite.tenant.ID,
			Metadata: map[string]string{},
			Data:     map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := suite.entityStore.MatchEvent(suite.ctx, event)
		require.NoError(t, err)
		require.Len(t, matchedDestinationSummaryList, 4)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(t, []string{suite.destinations[0].ID, suite.destinations[1].ID, suite.destinations[2].ID, suite.destinations[4].ID}, summary.ID)
		}

		// Match user.updated
		event = models.Event{
			ID:       uuid.New().String(),
			Topic:    "user.updated",
			Time:     time.Now(),
			TenantID: suite.tenant.ID,
			Metadata: map[string]string{},
			Data:     map[string]interface{}{},
		}
		matchedDestinationSummaryList, err = suite.entityStore.MatchEvent(suite.ctx, event)
		require.NoError(t, err)
		require.Len(t, matchedDestinationSummaryList, 2)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(t, []string{suite.destinations[0].ID, suite.destinations[4].ID}, summary.ID)
		}
	})
}

func TestDestinationEnableDisable(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
	)

	input := testutil.DestinationFactory.Any()
	require.NoError(t, entityStore.UpsertDestination(context.Background(), input))

	assertDestination := func(t *testing.T, expected models.Destination) {
		actual, err := entityStore.RetrieveDestination(context.Background(), input.TenantID, input.ID)
		require.NoError(t, err)
		assert.Equal(t, expected.ID, actual.ID)
		assert.True(t, cmp.Equal(expected.DisabledAt, actual.DisabledAt), "expected %v, got %v", expected.DisabledAt, actual.DisabledAt)
	}

	t.Run("should disable", func(t *testing.T) {
		now := time.Now()
		input.DisabledAt = &now
		require.NoError(t, entityStore.UpsertDestination(context.Background(), input))
		assertDestination(t, input)
	})

	t.Run("should enable", func(t *testing.T) {
		input.DisabledAt = nil
		require.NoError(t, entityStore.UpsertDestination(context.Background(), input))
		assertDestination(t, input)
	})
}

func TestMultiSuite_DisableAndMatch(t *testing.T) {
	t.Parallel()

	suite := multiDestinationSuite{}
	suite.SetupTest(t)

	t.Run("initial match user.deleted", func(t *testing.T) {
		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithTenantID(suite.tenant.ID),
			testutil.EventFactory.WithTopic("user.deleted"),
		)
		matchedDestinationSummaryList, err := suite.entityStore.MatchEvent(suite.ctx, event)
		require.NoError(t, err)
		require.Len(t, matchedDestinationSummaryList, 2)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(t, []string{suite.destinations[0].ID, suite.destinations[3].ID}, summary.ID)
		}
	})

	t.Run("should not match disabled destination", func(t *testing.T) {
		destination := suite.destinations[0]
		now := time.Now()
		destination.DisabledAt = &now
		require.NoError(t, suite.entityStore.UpsertDestination(suite.ctx, destination))

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithTenantID(suite.tenant.ID),
			testutil.EventFactory.WithTopic("user.deleted"),
		)
		matchedDestinationSummaryList, err := suite.entityStore.MatchEvent(suite.ctx, event)
		require.NoError(t, err)
		require.Len(t, matchedDestinationSummaryList, 1)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(t, []string{suite.destinations[3].ID}, summary.ID)
		}
	})

	t.Run("should match after re-enabled destination", func(t *testing.T) {
		destination := suite.destinations[0]
		destination.DisabledAt = nil
		require.NoError(t, suite.entityStore.UpsertDestination(suite.ctx, destination))

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithTenantID(suite.tenant.ID),
			testutil.EventFactory.WithTopic("user.deleted"),
		)
		matchedDestinationSummaryList, err := suite.entityStore.MatchEvent(suite.ctx, event)
		require.NoError(t, err)
		require.Len(t, matchedDestinationSummaryList, 2)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(t, []string{suite.destinations[0].ID, suite.destinations[3].ID}, summary.ID)
		}
	})
}

func TestEntityStore_DeleteDestination(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
	)

	destination := testutil.DestinationFactory.Any()
	require.NoError(t, entityStore.CreateDestination(ctx, destination))

	t.Run("should not return error when deleting existing destination", func(t *testing.T) {
		assert.NoError(t, entityStore.DeleteDestination(ctx, destination.TenantID, destination.ID))
	})

	t.Run("should not return error when deleting already-deleted destination", func(t *testing.T) {
		assert.NoError(t, entityStore.DeleteDestination(ctx, destination.TenantID, destination.ID))
	})

	t.Run("should return error when deleting non-existent destination", func(t *testing.T) {
		err := entityStore.DeleteDestination(ctx, destination.TenantID, uuid.New().String())
		assert.ErrorIs(t, err, models.ErrDestinationNotFound)
	})

	t.Run("should return ErrDestinationDeleted when retrieving deleted destination", func(t *testing.T) {
		dest, err := entityStore.RetrieveDestination(ctx, destination.TenantID, destination.ID)
		assert.ErrorIs(t, err, models.ErrDestinationDeleted)
		assert.Nil(t, dest)
	})

	t.Run("should not return deleted destination in list", func(t *testing.T) {
		destinations, err := entityStore.ListDestinationByTenant(ctx, destination.TenantID)
		assert.NoError(t, err)
		assert.Empty(t, destinations)
	})
}

func TestMultiSuite_DeleteAndMatch(t *testing.T) {
	t.Parallel()

	suite := multiDestinationSuite{}
	suite.SetupTest(t)

	t.Run("delete first destination", func(t *testing.T) {
		require.NoError(t,
			suite.entityStore.DeleteDestination(suite.ctx, suite.tenant.ID, suite.destinations[0].ID),
		)
	})

	t.Run("match event", func(t *testing.T) {
		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithTenantID(suite.tenant.ID),
			testutil.EventFactory.WithTopic("user.created"),
		)

		matchedDestinationSummaryList, err := suite.entityStore.MatchEvent(suite.ctx, event)
		require.NoError(t, err)
		require.Len(t, matchedDestinationSummaryList, 2)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(t, []string{suite.destinations[1].ID, suite.destinations[4].ID}, summary.ID)
		}
	})
}

func TestEntityStore_MaxDestinationsPerTenant(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	maxDestinations := 2
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
		models.WithMaxDestinationsPerTenant(maxDestinations),
	)

	tenant := models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
	require.NoError(t, entityStore.UpsertTenant(context.Background(), tenant))

	// Should be able to create up to maxDestinations
	for i := 0; i < maxDestinations; i++ {
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenant.ID),
		)
		err := entityStore.CreateDestination(context.Background(), destination)
		require.NoError(t, err, "Should be able to create destination %d", i+1)
	}

	// Should fail when trying to create one more
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	err := entityStore.CreateDestination(context.Background(), destination)
	require.Error(t, err)
	require.ErrorIs(t, err, models.ErrMaxDestinationsPerTenantReached)

	// Should be able to create after deleting one
	destinations, err := entityStore.ListDestinationByTenant(context.Background(), tenant.ID)
	require.NoError(t, err)
	require.NoError(t, entityStore.DeleteDestination(context.Background(), tenant.ID, destinations[0].ID))

	destination = testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	err = entityStore.CreateDestination(context.Background(), destination)
	require.NoError(t, err, "Should be able to create destination after deleting one")
}
