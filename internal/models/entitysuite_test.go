package models_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// Helper function used by test suite
func assertEqualDestination(t *testing.T, expected, actual models.Destination) {
	assert.Equal(t, expected.ID, actual.ID)
	assert.Equal(t, expected.Type, actual.Type)
	assert.Equal(t, expected.Topics, actual.Topics)
	assert.Equal(t, expected.Config, actual.Config)
	assert.Equal(t, expected.Credentials, actual.Credentials)
	assert.Equal(t, expected.DeliveryMetadata, actual.DeliveryMetadata)
	assert.Equal(t, expected.Metadata, actual.Metadata)
	assert.True(t, cmp.Equal(expected.CreatedAt, actual.CreatedAt))
	assert.True(t, cmp.Equal(expected.UpdatedAt, actual.UpdatedAt))
	assert.True(t, cmp.Equal(expected.DisabledAt, actual.DisabledAt))
}

// EntityTestSuite contains all entity store tests
type EntityTestSuite struct {
	suite.Suite
	ctx          context.Context
	redisClient  redis.Cmdable
	entityStore  models.EntityStore
	deploymentID string
}

func (s *EntityTestSuite) SetupTest() {
	s.ctx = context.Background()
	s.redisClient = testutil.CreateTestRedisClient(s.T())

	opts := []models.EntityStoreOption{
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
	}
	if s.deploymentID != "" {
		opts = append(opts, models.WithDeploymentID(s.deploymentID))
	}
	s.entityStore = models.NewEntityStore(s.redisClient, opts...)
}

func (s *EntityTestSuite) TestTenantCRUD() {
	t := s.T()
	now := time.Now()
	input := models.Tenant{
		ID:        idgen.String(),
		CreatedAt: now,
		UpdatedAt: now,
	}

	t.Run("gets empty", func(t *testing.T) {
		actual, err := s.entityStore.RetrieveTenant(s.ctx, input.ID)
		assert.Nil(s.T(), actual)
		assert.NoError(s.T(), err)
	})

	t.Run("sets", func(t *testing.T) {
		err := s.entityStore.UpsertTenant(s.ctx, input)
		require.NoError(s.T(), err)

		retrieved, err := s.entityStore.RetrieveTenant(s.ctx, input.ID)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), input.ID, retrieved.ID)
		assert.True(s.T(), input.CreatedAt.Equal(retrieved.CreatedAt))
	})

	t.Run("gets", func(t *testing.T) {
		actual, err := s.entityStore.RetrieveTenant(s.ctx, input.ID)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), input.ID, actual.ID)
		assert.True(s.T(), input.CreatedAt.Equal(actual.CreatedAt))
	})

	t.Run("overrides", func(t *testing.T) {
		input.CreatedAt = time.Now()

		err := s.entityStore.UpsertTenant(s.ctx, input)
		require.NoError(s.T(), err)

		actual, err := s.entityStore.RetrieveTenant(s.ctx, input.ID)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), input.ID, actual.ID)
		assert.True(s.T(), input.CreatedAt.Equal(actual.CreatedAt))
	})

	t.Run("clears", func(t *testing.T) {
		require.NoError(s.T(), s.entityStore.DeleteTenant(s.ctx, input.ID))

		actual, err := s.entityStore.RetrieveTenant(s.ctx, input.ID)
		assert.ErrorIs(s.T(), err, models.ErrTenantDeleted)
		assert.Nil(s.T(), actual)
	})

	t.Run("deletes again", func(t *testing.T) {
		assert.NoError(s.T(), s.entityStore.DeleteTenant(s.ctx, input.ID))
	})

	t.Run("deletes non-existent", func(t *testing.T) {
		assert.ErrorIs(s.T(), s.entityStore.DeleteTenant(s.ctx, "non-existent-tenant"), models.ErrTenantNotFound)
	})

	t.Run("creates & overrides deleted resource", func(t *testing.T) {
		require.NoError(s.T(), s.entityStore.UpsertTenant(s.ctx, input))

		actual, err := s.entityStore.RetrieveTenant(s.ctx, input.ID)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), input.ID, actual.ID)
		assert.True(s.T(), input.CreatedAt.Equal(actual.CreatedAt))
	})

	t.Run("upserts with metadata", func(t *testing.T) {
		input.Metadata = map[string]string{
			"environment": "production",
			"team":        "platform",
		}

		err := s.entityStore.UpsertTenant(s.ctx, input)
		require.NoError(s.T(), err)

		retrieved, err := s.entityStore.RetrieveTenant(s.ctx, input.ID)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), input.ID, retrieved.ID)
		assert.Equal(s.T(), input.Metadata, retrieved.Metadata)
	})

	t.Run("updates metadata", func(t *testing.T) {
		input.Metadata = map[string]string{
			"environment": "staging",
			"team":        "engineering",
			"region":      "us-west-2",
		}

		err := s.entityStore.UpsertTenant(s.ctx, input)
		require.NoError(s.T(), err)

		retrieved, err := s.entityStore.RetrieveTenant(s.ctx, input.ID)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), input.Metadata, retrieved.Metadata)
	})

	t.Run("handles nil metadata", func(t *testing.T) {
		input.Metadata = nil

		err := s.entityStore.UpsertTenant(s.ctx, input)
		require.NoError(s.T(), err)

		retrieved, err := s.entityStore.RetrieveTenant(s.ctx, input.ID)
		require.NoError(s.T(), err)
		assert.Nil(s.T(), retrieved.Metadata)
	})

	// UpdatedAt tests
	t.Run("sets updated_at on create", func(t *testing.T) {
		newTenant := testutil.TenantFactory.Any()

		err := s.entityStore.UpsertTenant(s.ctx, newTenant)
		require.NoError(s.T(), err)

		retrieved, err := s.entityStore.RetrieveTenant(s.ctx, newTenant.ID)
		require.NoError(s.T(), err)
		assert.True(s.T(), newTenant.UpdatedAt.Unix() == retrieved.UpdatedAt.Unix())
	})

	t.Run("updates updated_at on upsert", func(t *testing.T) {
		original := testutil.TenantFactory.Any()

		err := s.entityStore.UpsertTenant(s.ctx, original)
		require.NoError(s.T(), err)

		// Wait a bit to ensure different timestamp
		time.Sleep(10 * time.Millisecond)

		// Update the tenant
		updated := original
		updated.UpdatedAt = time.Now()

		err = s.entityStore.UpsertTenant(s.ctx, updated)
		require.NoError(s.T(), err)

		retrieved, err := s.entityStore.RetrieveTenant(s.ctx, updated.ID)
		require.NoError(s.T(), err)

		// updated_at should be newer than original
		assert.True(s.T(), retrieved.UpdatedAt.After(original.UpdatedAt))
		assert.True(s.T(), updated.UpdatedAt.Unix() == retrieved.UpdatedAt.Unix())
	})

	t.Run("fallback updated_at to created_at for existing records", func(t *testing.T) {
		// Create a tenant normally first
		oldTenant := testutil.TenantFactory.Any()
		err := s.entityStore.UpsertTenant(s.ctx, oldTenant)
		require.NoError(s.T(), err)

		// Now manually remove the updated_at field from Redis to simulate old record
		key := "tenant:" + oldTenant.ID
		err = s.redisClient.HDel(s.ctx, key, "updated_at").Err()
		require.NoError(s.T(), err)

		// Retrieve should fallback updated_at to created_at
		retrieved, err := s.entityStore.RetrieveTenant(s.ctx, oldTenant.ID)
		require.NoError(s.T(), err)
		assert.True(s.T(), retrieved.UpdatedAt.Equal(retrieved.CreatedAt))
	})
}

func (s *EntityTestSuite) TestDestinationCRUD() {
	t := s.T()
	now := time.Now()
	input := models.Destination{
		ID:     idgen.Destination(),
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
		DeliveryMetadata: map[string]string{
			"app-id": "test-app",
			"source": "outpost",
		},
		Metadata: map[string]string{
			"environment": "test",
			"team":        "platform",
		},
		CreatedAt:  now,
		UpdatedAt:  now,
		DisabledAt: nil,
		TenantID:   idgen.String(),
	}

	t.Run("gets empty", func(t *testing.T) {
		actual, err := s.entityStore.RetrieveDestination(s.ctx, input.TenantID, input.ID)
		require.NoError(s.T(), err)
		assert.Nil(s.T(), actual)
	})

	t.Run("sets", func(t *testing.T) {
		err := s.entityStore.CreateDestination(s.ctx, input)
		require.NoError(s.T(), err)
	})

	t.Run("gets", func(t *testing.T) {
		actual, err := s.entityStore.RetrieveDestination(s.ctx, input.TenantID, input.ID)
		require.NoError(s.T(), err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("updates", func(t *testing.T) {
		input.Topics = []string{"*"}
		input.DeliveryMetadata = map[string]string{
			"app-id":  "updated-app",
			"version": "2.0",
		}
		input.Metadata = map[string]string{
			"environment": "staging",
		}

		err := s.entityStore.UpsertDestination(s.ctx, input)
		require.NoError(s.T(), err)

		actual, err := s.entityStore.RetrieveDestination(s.ctx, input.TenantID, input.ID)
		require.NoError(s.T(), err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("clears", func(t *testing.T) {
		err := s.entityStore.DeleteDestination(s.ctx, input.TenantID, input.ID)
		require.NoError(s.T(), err)

		actual, err := s.entityStore.RetrieveDestination(s.ctx, input.TenantID, input.ID)
		assert.ErrorIs(s.T(), err, models.ErrDestinationDeleted)
		assert.Nil(s.T(), actual)
	})

	t.Run("creates & overrides deleted resource", func(t *testing.T) {
		err := s.entityStore.CreateDestination(s.ctx, input)
		require.NoError(s.T(), err)

		actual, err := s.entityStore.RetrieveDestination(s.ctx, input.TenantID, input.ID)
		require.NoError(s.T(), err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("err when creates duplicate", func(t *testing.T) {
		assert.ErrorIs(s.T(), s.entityStore.CreateDestination(s.ctx, input), models.ErrDuplicateDestination)

		// cleanup
		require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, input.TenantID, input.ID))
	})

	t.Run("handles nil delivery_metadata and metadata", func(t *testing.T) {
		// Factory defaults to nil for DeliveryMetadata and Metadata
		inputWithNilFields := testutil.DestinationFactory.Any()

		err := s.entityStore.CreateDestination(s.ctx, inputWithNilFields)
		require.NoError(s.T(), err)

		actual, err := s.entityStore.RetrieveDestination(s.ctx, inputWithNilFields.TenantID, inputWithNilFields.ID)
		require.NoError(s.T(), err)
		assert.Nil(t, actual.DeliveryMetadata)
		assert.Nil(t, actual.Metadata)

		// cleanup
		require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, inputWithNilFields.TenantID, inputWithNilFields.ID))
	})

	// UpdatedAt tests
	t.Run("sets updated_at on create", func(t *testing.T) {
		newDest := testutil.DestinationFactory.Any()

		err := s.entityStore.CreateDestination(s.ctx, newDest)
		require.NoError(s.T(), err)

		retrieved, err := s.entityStore.RetrieveDestination(s.ctx, newDest.TenantID, newDest.ID)
		require.NoError(s.T(), err)
		assert.True(s.T(), newDest.UpdatedAt.Unix() == retrieved.UpdatedAt.Unix())

		// cleanup
		require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, newDest.TenantID, newDest.ID))
	})

	t.Run("updates updated_at on upsert", func(t *testing.T) {
		original := testutil.DestinationFactory.Any()

		err := s.entityStore.CreateDestination(s.ctx, original)
		require.NoError(s.T(), err)

		// Wait a bit to ensure different timestamp
		time.Sleep(10 * time.Millisecond)

		// Update the destination
		updated := original
		updated.UpdatedAt = time.Now()
		updated.Topics = []string{"updated.topic"}

		err = s.entityStore.UpsertDestination(s.ctx, updated)
		require.NoError(s.T(), err)

		retrieved, err := s.entityStore.RetrieveDestination(s.ctx, updated.TenantID, updated.ID)
		require.NoError(s.T(), err)

		// updated_at should be newer than original
		assert.True(s.T(), retrieved.UpdatedAt.After(original.UpdatedAt))
		assert.True(s.T(), updated.UpdatedAt.Unix() == retrieved.UpdatedAt.Unix())

		// cleanup
		require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, updated.TenantID, updated.ID))
	})

	t.Run("fallback updated_at to created_at for existing records", func(t *testing.T) {
		// Create a destination normally first
		oldDest := testutil.DestinationFactory.Any()
		err := s.entityStore.CreateDestination(s.ctx, oldDest)
		require.NoError(s.T(), err)

		// Now manually remove the updated_at field from Redis to simulate old record
		key := "destination:" + oldDest.TenantID + ":" + oldDest.ID
		err = s.redisClient.HDel(s.ctx, key, "updated_at").Err()
		require.NoError(s.T(), err)

		// Retrieve should fallback updated_at to created_at
		retrieved, err := s.entityStore.RetrieveDestination(s.ctx, oldDest.TenantID, oldDest.ID)
		require.NoError(s.T(), err)
		assert.True(s.T(), retrieved.UpdatedAt.Equal(retrieved.CreatedAt))

		// cleanup
		require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, oldDest.TenantID, oldDest.ID))
	})
}

func (s *EntityTestSuite) TestListDestinationEmpty() {
	destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, idgen.String())
	require.NoError(s.T(), err)
	assert.Empty(s.T(), destinations)
}

func (s *EntityTestSuite) TestDeleteTenantAndAssociatedDestinations() {
	tenant := models.Tenant{
		ID:        idgen.String(),
		CreatedAt: time.Now(),
	}
	// Arrange
	require.NoError(s.T(), s.entityStore.UpsertTenant(s.ctx, tenant))
	destinationIDs := []string{idgen.Destination(), idgen.Destination(), idgen.Destination()}
	for _, id := range destinationIDs {
		require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx, testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(id),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
		)))
	}
	// Act
	require.NoError(s.T(), s.entityStore.DeleteTenant(s.ctx, tenant.ID))
	// Assert
	_, err := s.entityStore.RetrieveTenant(s.ctx, tenant.ID)
	assert.ErrorIs(s.T(), err, models.ErrTenantDeleted)
	for _, id := range destinationIDs {
		_, err := s.entityStore.RetrieveDestination(s.ctx, tenant.ID, id)
		assert.ErrorIs(s.T(), err, models.ErrDestinationDeleted)
	}
}

// Helper struct for multi-destination tests
type multiDestinationData struct {
	tenant       models.Tenant
	destinations []models.Destination
}

func (s *EntityTestSuite) setupMultiDestination() multiDestinationData {
	data := multiDestinationData{
		tenant: models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		},
		destinations: make([]models.Destination, 5),
	}
	require.NoError(s.T(), s.entityStore.UpsertTenant(s.ctx, data.tenant))

	destinationTopicList := [][]string{
		{"*"},
		{"user.created"},
		{"user.updated"},
		{"user.deleted"},
		{"user.created", "user.updated"},
	}
	for i := 0; i < 5; i++ {
		id := idgen.Destination()
		data.destinations[i] = testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(id),
			testutil.DestinationFactory.WithTenantID(data.tenant.ID),
			testutil.DestinationFactory.WithTopics(destinationTopicList[i]),
		)
		require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx, data.destinations[i]))
	}

	// Insert & Delete destination to ensure it's cleaned up properly
	toBeDeletedID := idgen.Destination()
	require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx,
		testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(toBeDeletedID),
			testutil.DestinationFactory.WithTenantID(data.tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
		)))
	require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, data.tenant.ID, toBeDeletedID))

	return data
}

func (s *EntityTestSuite) TestMultiDestinationRetrieveTenantDestinationsCount() {
	data := s.setupMultiDestination()

	tenant, err := s.entityStore.RetrieveTenant(s.ctx, data.tenant.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 5, tenant.DestinationsCount)
}

func (s *EntityTestSuite) TestMultiDestinationRetrieveTenantTopics() {
	data := s.setupMultiDestination()

	tenant, err := s.entityStore.RetrieveTenant(s.ctx, data.tenant.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

	require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, data.tenant.ID, data.destinations[0].ID))
	tenant, err = s.entityStore.RetrieveTenant(s.ctx, data.tenant.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

	require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, data.tenant.ID, data.destinations[1].ID))
	tenant, err = s.entityStore.RetrieveTenant(s.ctx, data.tenant.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

	require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, data.tenant.ID, data.destinations[2].ID))
	tenant, err = s.entityStore.RetrieveTenant(s.ctx, data.tenant.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

	require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, data.tenant.ID, data.destinations[3].ID))
	tenant, err = s.entityStore.RetrieveTenant(s.ctx, data.tenant.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{"user.created", "user.updated"}, tenant.Topics)

	require.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, data.tenant.ID, data.destinations[4].ID))
	tenant, err = s.entityStore.RetrieveTenant(s.ctx, data.tenant.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), []string{}, tenant.Topics)
}

func (s *EntityTestSuite) TestMultiDestinationListDestinationByTenant() {
	data := s.setupMultiDestination()

	destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, data.tenant.ID)
	require.NoError(s.T(), err)
	require.Len(s.T(), destinations, 5)
	for index, destination := range destinations {
		require.Equal(s.T(), data.destinations[index].ID, destination.ID)
	}
}

func (s *EntityTestSuite) TestMultiDestinationListDestinationWithOpts() {
	t := s.T()
	data := s.setupMultiDestination()

	t.Run("filter by type: webhook", func(t *testing.T) {
		destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, data.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Type: []string{"webhook"},
		}))
		require.NoError(s.T(), err)
		require.Len(s.T(), destinations, 5)
	})

	t.Run("filter by type: rabbitmq", func(t *testing.T) {
		destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, data.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Type: []string{"rabbitmq"},
		}))
		require.NoError(s.T(), err)
		require.Len(s.T(), destinations, 0)
	})

	t.Run("filter by type: webhook,rabbitmq", func(t *testing.T) {
		destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, data.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Type: []string{"webhook", "rabbitmq"},
		}))
		require.NoError(s.T(), err)
		require.Len(s.T(), destinations, 5)
	})

	t.Run("filter by topic: user.created", func(t *testing.T) {
		destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, data.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Topics: []string{"user.created"},
		}))
		require.NoError(s.T(), err)
		require.Len(s.T(), destinations, 3)
	})

	t.Run("filter by topic: user.created,user.updated", func(t *testing.T) {
		destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, data.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Topics: []string{"user.created", "user.updated"},
		}))
		require.NoError(s.T(), err)
		require.Len(s.T(), destinations, 2)
	})

	t.Run("filter by type: rabbitmq, topic: user.created,user.updated", func(t *testing.T) {
		destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, data.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Type:   []string{"rabbitmq"},
			Topics: []string{"user.created", "user.updated"},
		}))
		require.NoError(s.T(), err)
		require.Len(s.T(), destinations, 0)
	})

	t.Run("filter by topic: *", func(t *testing.T) {
		destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, data.tenant.ID, models.WithDestinationFilter(models.DestinationFilter{
			Topics: []string{"*"},
		}))
		require.NoError(s.T(), err)
		require.Len(s.T(), destinations, 1)
	})
}

func (s *EntityTestSuite) TestMultiDestinationMatchEvent() {
	t := s.T()
	data := s.setupMultiDestination()

	t.Run("match by topic", func(t *testing.T) {
		event := models.Event{
			ID:       idgen.Event(),
			Topic:    "user.created",
			Time:     time.Now(),
			TenantID: data.tenant.ID,
			Metadata: map[string]string{},
			Data:     map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)

		require.Len(s.T(), matchedDestinationSummaryList, 3)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(s.T(), []string{data.destinations[0].ID, data.destinations[1].ID, data.destinations[4].ID}, summary.ID)
		}
	})

	t.Run("match by topic & destination", func(t *testing.T) {
		event := models.Event{
			ID:            idgen.Event(),
			Topic:         "user.created",
			Time:          time.Now(),
			TenantID:      data.tenant.ID,
			DestinationID: data.destinations[1].ID,
			Metadata:      map[string]string{},
			Data:          map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)

		require.Len(s.T(), matchedDestinationSummaryList, 1)
		require.Equal(s.T(), data.destinations[1].ID, matchedDestinationSummaryList[0].ID)
	})

	t.Run("destination not found", func(t *testing.T) {
		event := models.Event{
			ID:            idgen.Event(),
			Topic:         "user.created",
			Time:          time.Now(),
			TenantID:      data.tenant.ID,
			DestinationID: "not-found",
			Metadata:      map[string]string{},
			Data:          map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)

		require.Len(s.T(), matchedDestinationSummaryList, 0)
	})

	t.Run("destination topic is invalid", func(t *testing.T) {
		event := models.Event{
			ID:            idgen.Event(),
			Topic:         "user.created",
			Time:          time.Now(),
			TenantID:      data.tenant.ID,
			DestinationID: data.destinations[3].ID, // "user-deleted" destination
			Metadata:      map[string]string{},
			Data:          map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)

		require.Len(s.T(), matchedDestinationSummaryList, 0)
	})

	t.Run("match after destination is updated", func(t *testing.T) {
		updatedIndex := 2
		updatedTopics := []string{"user.created"}
		updatedDestination := data.destinations[updatedIndex]
		updatedDestination.Topics = updatedTopics
		require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx, updatedDestination))

		actual, err := s.entityStore.RetrieveDestination(s.ctx, updatedDestination.TenantID, updatedDestination.ID)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), updatedDestination.Topics, actual.Topics)

		destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, data.tenant.ID)
		require.NoError(s.T(), err)
		assert.Len(s.T(), destinations, 5)

		// Match user.created
		event := models.Event{
			ID:       idgen.Event(),
			Topic:    "user.created",
			Time:     time.Now(),
			TenantID: data.tenant.ID,
			Metadata: map[string]string{},
			Data:     map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)
		require.Len(s.T(), matchedDestinationSummaryList, 4)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(s.T(), []string{data.destinations[0].ID, data.destinations[1].ID, data.destinations[2].ID, data.destinations[4].ID}, summary.ID)
		}

		// Match user.updated
		event = models.Event{
			ID:       idgen.Event(),
			Topic:    "user.updated",
			Time:     time.Now(),
			TenantID: data.tenant.ID,
			Metadata: map[string]string{},
			Data:     map[string]interface{}{},
		}
		matchedDestinationSummaryList, err = s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)
		require.Len(s.T(), matchedDestinationSummaryList, 2)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(s.T(), []string{data.destinations[0].ID, data.destinations[4].ID}, summary.ID)
		}
	})
}

func (s *EntityTestSuite) TestDestinationEnableDisable() {
	t := s.T()
	input := testutil.DestinationFactory.Any()
	require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx, input))

	assertDestination := func(t *testing.T, expected models.Destination) {
		actual, err := s.entityStore.RetrieveDestination(s.ctx, input.TenantID, input.ID)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), expected.ID, actual.ID)
		assert.True(s.T(), cmp.Equal(expected.DisabledAt, actual.DisabledAt), "expected %v, got %v", expected.DisabledAt, actual.DisabledAt)
	}

	t.Run("should disable", func(t *testing.T) {
		now := time.Now()
		input.DisabledAt = &now
		require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx, input))
		assertDestination(t, input)
	})

	t.Run("should enable", func(t *testing.T) {
		input.DisabledAt = nil
		require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx, input))
		assertDestination(t, input)
	})
}

func (s *EntityTestSuite) TestMultiSuiteDisableAndMatch() {
	t := s.T()
	data := s.setupMultiDestination()

	t.Run("initial match user.deleted", func(t *testing.T) {
		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithTenantID(data.tenant.ID),
			testutil.EventFactory.WithTopic("user.deleted"),
		)
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)
		require.Len(s.T(), matchedDestinationSummaryList, 2)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(s.T(), []string{data.destinations[0].ID, data.destinations[3].ID}, summary.ID)
		}
	})

	t.Run("should not match disabled destination", func(t *testing.T) {
		destination := data.destinations[0]
		now := time.Now()
		destination.DisabledAt = &now
		require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx, destination))

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithTenantID(data.tenant.ID),
			testutil.EventFactory.WithTopic("user.deleted"),
		)
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)
		require.Len(s.T(), matchedDestinationSummaryList, 1)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(s.T(), []string{data.destinations[3].ID}, summary.ID)
		}
	})

	t.Run("should match after re-enabled destination", func(t *testing.T) {
		destination := data.destinations[0]
		destination.DisabledAt = nil
		require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx, destination))

		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithTenantID(data.tenant.ID),
			testutil.EventFactory.WithTopic("user.deleted"),
		)
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)
		require.Len(s.T(), matchedDestinationSummaryList, 2)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(s.T(), []string{data.destinations[0].ID, data.destinations[3].ID}, summary.ID)
		}
	})
}

func (s *EntityTestSuite) TestDeleteDestination() {
	t := s.T()
	destination := testutil.DestinationFactory.Any()
	require.NoError(s.T(), s.entityStore.CreateDestination(s.ctx, destination))

	t.Run("should not return error when deleting existing destination", func(t *testing.T) {
		assert.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, destination.TenantID, destination.ID))
	})

	t.Run("should not return error when deleting already-deleted destination", func(t *testing.T) {
		assert.NoError(s.T(), s.entityStore.DeleteDestination(s.ctx, destination.TenantID, destination.ID))
	})

	t.Run("should return error when deleting non-existent destination", func(t *testing.T) {
		err := s.entityStore.DeleteDestination(s.ctx, destination.TenantID, idgen.Destination())
		assert.ErrorIs(s.T(), err, models.ErrDestinationNotFound)
	})

	t.Run("should return ErrDestinationDeleted when retrieving deleted destination", func(t *testing.T) {
		dest, err := s.entityStore.RetrieveDestination(s.ctx, destination.TenantID, destination.ID)
		assert.ErrorIs(s.T(), err, models.ErrDestinationDeleted)
		assert.Nil(s.T(), dest)
	})

	t.Run("should not return deleted destination in list", func(t *testing.T) {
		destinations, err := s.entityStore.ListDestinationByTenant(s.ctx, destination.TenantID)
		assert.NoError(s.T(), err)
		assert.Empty(s.T(), destinations)
	})
}

func (s *EntityTestSuite) TestMultiSuiteDeleteAndMatch() {
	t := s.T()
	data := s.setupMultiDestination()

	t.Run("delete first destination", func(t *testing.T) {
		require.NoError(s.T(),
			s.entityStore.DeleteDestination(s.ctx, data.tenant.ID, data.destinations[0].ID),
		)
	})

	t.Run("match event", func(t *testing.T) {
		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithTenantID(data.tenant.ID),
			testutil.EventFactory.WithTopic("user.created"),
		)

		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)
		require.Len(s.T(), matchedDestinationSummaryList, 2)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(s.T(), []string{data.destinations[1].ID, data.destinations[4].ID}, summary.ID)
		}
	})
}
