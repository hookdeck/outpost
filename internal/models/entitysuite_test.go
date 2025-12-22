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
	assert.Equal(t, expected.Filter, actual.Filter)
	assert.Equal(t, expected.Config, actual.Config)
	assert.Equal(t, expected.Credentials, actual.Credentials)
	assert.Equal(t, expected.DeliveryMetadata, actual.DeliveryMetadata)
	assert.Equal(t, expected.Metadata, actual.Metadata)
	assert.True(t, cmp.Equal(expected.CreatedAt, actual.CreatedAt))
	assert.True(t, cmp.Equal(expected.UpdatedAt, actual.UpdatedAt))
	assert.True(t, cmp.Equal(expected.DisabledAt, actual.DisabledAt))
}

// RedisClientFactory creates a Redis client for testing.
// Required - each test suite must explicitly provide one.
type RedisClientFactory func(t *testing.T) redis.Cmdable

// EntityTestSuite contains all entity store tests.
// Requires a RedisClientFactory to be set before running.
type EntityTestSuite struct {
	suite.Suite
	ctx                context.Context
	redisClient        redis.Cmdable
	entityStore        models.EntityStore
	deploymentID       string
	RedisClientFactory RedisClientFactory // Required - must be set
}

func (s *EntityTestSuite) SetupTest() {
	s.ctx = context.Background()

	require.NotNil(s.T(), s.RedisClientFactory, "RedisClientFactory must be set")
	s.redisClient = s.RedisClientFactory(s.T())

	opts := []models.EntityStoreOption{
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
	}
	if s.deploymentID != "" {
		opts = append(opts, models.WithDeploymentID(s.deploymentID))
	}
	s.entityStore = models.NewEntityStore(s.redisClient, opts...)

	// Initialize entity store (probes for RediSearch)
	err := s.entityStore.Init(s.ctx)
	require.NoError(s.T(), err)
}

func (s *EntityTestSuite) TestInitIdempotency() {
	// Calling Init multiple times should not fail (index already exists is handled gracefully)
	for i := 0; i < 3; i++ {
		err := s.entityStore.Init(s.ctx)
		require.NoError(s.T(), err, "Init call %d should not fail", i+1)
	}
}

func (s *EntityTestSuite) TestListTenantNotSupported() {
	// This test verifies behavior when RediSearch is NOT available (miniredis case)
	// When running with Redis Stack, this test will pass but ListTenant will work
	// When running with miniredis, ListTenant should return ErrListTenantNotSupported

	_, err := s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{})

	// Check if we're on miniredis (no RediSearch support)
	// We can detect this by checking if the error is ErrListTenantNotSupported
	if err != nil {
		assert.ErrorIs(s.T(), err, models.ErrListTenantNotSupported,
			"ListTenant should return ErrListTenantNotSupported when RediSearch is not available")
	}
	// If err is nil, we're on Redis Stack and ListTenant works - that's fine too
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

	// MatchEvent IGNORES destination_id and only matches by topic.
	// These tests verify that destination_id in the event is intentionally ignored.
	// Specific destination matching is handled at a higher level (publishmq package).
	t.Run("ignores destination_id and matches by topic only", func(t *testing.T) {
		event := models.Event{
			ID:            idgen.Event(),
			Topic:         "user.created",
			Time:          time.Now(),
			TenantID:      data.tenant.ID,
			DestinationID: data.destinations[1].ID, // This should be IGNORED
			Metadata:      map[string]string{},
			Data:          map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)

		// Should match all destinations with "user.created" topic, not just the specified destination_id
		require.Len(s.T(), matchedDestinationSummaryList, 3)
		for _, summary := range matchedDestinationSummaryList {
			require.Contains(s.T(), []string{data.destinations[0].ID, data.destinations[1].ID, data.destinations[4].ID}, summary.ID)
		}
	})

	t.Run("ignores non-existent destination_id", func(t *testing.T) {
		event := models.Event{
			ID:            idgen.Event(),
			Topic:         "user.created",
			Time:          time.Now(),
			TenantID:      data.tenant.ID,
			DestinationID: "not-found", // This should be IGNORED
			Metadata:      map[string]string{},
			Data:          map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)

		// Should still match all destinations with "user.created" topic
		require.Len(s.T(), matchedDestinationSummaryList, 3)
	})

	t.Run("ignores destination_id with mismatched topic", func(t *testing.T) {
		event := models.Event{
			ID:            idgen.Event(),
			Topic:         "user.created",
			Time:          time.Now(),
			TenantID:      data.tenant.ID,
			DestinationID: data.destinations[3].ID, // "user.deleted" destination - should be IGNORED
			Metadata:      map[string]string{},
			Data:          map[string]interface{}{},
		}
		matchedDestinationSummaryList, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)

		// Should match all destinations with "user.created" topic, not the specified "user.deleted" destination
		require.Len(s.T(), matchedDestinationSummaryList, 3)
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

func (s *EntityTestSuite) TestDestinationFilterPersistence() {
	t := s.T()
	tenant := models.Tenant{ID: idgen.String()}
	require.NoError(s.T(), s.entityStore.UpsertTenant(s.ctx, tenant))

	t.Run("stores and retrieves destination with filter", func(t *testing.T) {
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
			testutil.DestinationFactory.WithFilter(models.Filter{
				"data": map[string]any{
					"type": "order.created",
				},
			}),
		)

		require.NoError(s.T(), s.entityStore.CreateDestination(s.ctx, destination))

		retrieved, err := s.entityStore.RetrieveDestination(s.ctx, tenant.ID, destination.ID)
		require.NoError(s.T(), err)
		assert.NotNil(s.T(), retrieved.Filter)
		assert.Equal(s.T(), "order.created", retrieved.Filter["data"].(map[string]any)["type"])
	})

	t.Run("stores destination with nil filter", func(t *testing.T) {
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
		)

		require.NoError(s.T(), s.entityStore.CreateDestination(s.ctx, destination))

		retrieved, err := s.entityStore.RetrieveDestination(s.ctx, tenant.ID, destination.ID)
		require.NoError(s.T(), err)
		assert.Nil(s.T(), retrieved.Filter)
	})

	t.Run("updates destination filter", func(t *testing.T) {
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
			testutil.DestinationFactory.WithFilter(models.Filter{
				"data": map[string]any{"type": "order.created"},
			}),
		)

		require.NoError(s.T(), s.entityStore.CreateDestination(s.ctx, destination))

		// Update filter
		destination.Filter = models.Filter{
			"data": map[string]any{"type": "order.updated"},
		}
		require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx, destination))

		retrieved, err := s.entityStore.RetrieveDestination(s.ctx, tenant.ID, destination.ID)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), "order.updated", retrieved.Filter["data"].(map[string]any)["type"])
	})

	t.Run("removes destination filter", func(t *testing.T) {
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
			testutil.DestinationFactory.WithFilter(models.Filter{
				"data": map[string]any{"type": "order.created"},
			}),
		)

		require.NoError(s.T(), s.entityStore.CreateDestination(s.ctx, destination))

		// Remove filter
		destination.Filter = nil
		require.NoError(s.T(), s.entityStore.UpsertDestination(s.ctx, destination))

		retrieved, err := s.entityStore.RetrieveDestination(s.ctx, tenant.ID, destination.ID)
		require.NoError(s.T(), err)
		assert.Nil(s.T(), retrieved.Filter)
	})
}

func (s *EntityTestSuite) TestMatchEventWithFilter() {
	t := s.T()
	tenant := models.Tenant{ID: idgen.String()}
	require.NoError(s.T(), s.entityStore.UpsertTenant(s.ctx, tenant))

	// Create destinations with different filters
	destNoFilter := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest_no_filter"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
		testutil.DestinationFactory.WithTopics([]string{"*"}),
	)

	destFilterOrderCreated := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest_filter_order_created"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
		testutil.DestinationFactory.WithTopics([]string{"*"}),
		testutil.DestinationFactory.WithFilter(models.Filter{
			"data": map[string]any{
				"type": "order.created",
			},
		}),
	)

	destFilterOrderUpdated := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest_filter_order_updated"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
		testutil.DestinationFactory.WithTopics([]string{"*"}),
		testutil.DestinationFactory.WithFilter(models.Filter{
			"data": map[string]any{
				"type": "order.updated",
			},
		}),
	)

	destFilterPremiumCustomer := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest_filter_premium"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
		testutil.DestinationFactory.WithTopics([]string{"*"}),
		testutil.DestinationFactory.WithFilter(models.Filter{
			"data": map[string]any{
				"customer": map[string]any{
					"tier": "premium",
				},
			},
		}),
	)

	require.NoError(s.T(), s.entityStore.CreateDestination(s.ctx, destNoFilter))
	require.NoError(s.T(), s.entityStore.CreateDestination(s.ctx, destFilterOrderCreated))
	require.NoError(s.T(), s.entityStore.CreateDestination(s.ctx, destFilterOrderUpdated))
	require.NoError(s.T(), s.entityStore.CreateDestination(s.ctx, destFilterPremiumCustomer))

	t.Run("event without filter field matches only destinations with matching filter", func(t *testing.T) {
		event := models.Event{
			ID:       idgen.Event(),
			TenantID: tenant.ID,
			Topic:    "order",
			Time:     time.Now(),
			Metadata: map[string]string{},
			Data: map[string]interface{}{
				"type": "order.created",
			},
		}

		matchedDestinations, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)

		// Should match: destNoFilter (no filter), destFilterOrderCreated (matches type)
		// Should NOT match: destFilterOrderUpdated (wrong type), destFilterPremiumCustomer (missing customer.tier)
		assert.Len(s.T(), matchedDestinations, 2)
		ids := []string{}
		for _, dest := range matchedDestinations {
			ids = append(ids, dest.ID)
		}
		assert.Contains(s.T(), ids, "dest_no_filter")
		assert.Contains(s.T(), ids, "dest_filter_order_created")
	})

	t.Run("event with nested data matches nested filter", func(t *testing.T) {
		event := models.Event{
			ID:       idgen.Event(),
			TenantID: tenant.ID,
			Topic:    "order",
			Time:     time.Now(),
			Metadata: map[string]string{},
			Data: map[string]interface{}{
				"type": "order.created",
				"customer": map[string]interface{}{
					"id":   "cust_123",
					"tier": "premium",
				},
			},
		}

		matchedDestinations, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)

		// Should match: destNoFilter, destFilterOrderCreated, destFilterPremiumCustomer
		// Should NOT match: destFilterOrderUpdated (wrong type)
		assert.Len(s.T(), matchedDestinations, 3)
		ids := []string{}
		for _, dest := range matchedDestinations {
			ids = append(ids, dest.ID)
		}
		assert.Contains(s.T(), ids, "dest_no_filter")
		assert.Contains(s.T(), ids, "dest_filter_order_created")
		assert.Contains(s.T(), ids, "dest_filter_premium")
	})

	t.Run("topic filter takes precedence before content filter", func(t *testing.T) {
		// Create a destination with specific topic AND filter
		destTopicAndFilter := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID("dest_topic_and_filter"),
			testutil.DestinationFactory.WithTenantID(tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"user.created"}), // Specific topic
			testutil.DestinationFactory.WithFilter(models.Filter{
				"data": map[string]any{
					"type": "order.created",
				},
			}),
		)
		require.NoError(s.T(), s.entityStore.CreateDestination(s.ctx, destTopicAndFilter))

		// Event with matching filter but wrong topic
		event := models.Event{
			ID:       idgen.Event(),
			TenantID: tenant.ID,
			Topic:    "order",
			Time:     time.Now(),
			Metadata: map[string]string{},
			Data: map[string]interface{}{
				"type": "order.created",
			},
		}

		matchedDestinations, err := s.entityStore.MatchEvent(s.ctx, event)
		require.NoError(s.T(), err)

		// Should NOT match destTopicAndFilter because topic doesn't match
		for _, dest := range matchedDestinations {
			assert.NotEqual(s.T(), "dest_topic_and_filter", dest.ID)
		}
	})
}

// =============================================================================
// ListTenantTestSuite - Tests for ListTenant functionality (requires RediSearch)
// =============================================================================

// ListTenantTestSuite tests ListTenant functionality.
// Only runs with Redis Stack since it requires RediSearch.
type ListTenantTestSuite struct {
	suite.Suite
	ctx                context.Context
	redisClient        redis.Cmdable
	entityStore        models.EntityStore
	deploymentID       string
	RedisClientFactory RedisClientFactory // Required - must be set
}

func (s *ListTenantTestSuite) SetupTest() {
	s.ctx = context.Background()

	require.NotNil(s.T(), s.RedisClientFactory, "RedisClientFactory must be set")
	s.redisClient = s.RedisClientFactory(s.T())

	opts := []models.EntityStoreOption{
		models.WithCipher(models.NewAESCipher("secret")),
		models.WithAvailableTopics(testutil.TestTopics),
	}
	if s.deploymentID != "" {
		opts = append(opts, models.WithDeploymentID(s.deploymentID))
	}
	s.entityStore = models.NewEntityStore(s.redisClient, opts...)

	// Initialize entity store (probes for RediSearch)
	err := s.entityStore.Init(s.ctx)
	require.NoError(s.T(), err)
}

func (s *ListTenantTestSuite) TestInitIdempotency() {
	// Calling Init multiple times should not fail
	for i := 0; i < 3; i++ {
		err := s.entityStore.Init(s.ctx)
		require.NoError(s.T(), err, "Init call %d should not fail", i+1)
	}
}

func (s *ListTenantTestSuite) TestListTenantEmpty() {
	resp, err := s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{})
	require.NoError(s.T(), err)
	assert.Empty(s.T(), resp.Data)
	assert.Empty(s.T(), resp.Next)
	assert.Empty(s.T(), resp.Prev)
}

func (s *ListTenantTestSuite) TestListTenantBasic() {
	// Create some tenants
	tenants := make([]models.Tenant, 5)
	for i := 0; i < 5; i++ {
		tenants[i] = models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
			UpdatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		require.NoError(s.T(), s.entityStore.UpsertTenant(s.ctx, tenants[i]))
	}

	// Wait a bit for indexing
	time.Sleep(100 * time.Millisecond)

	s.T().Run("lists all tenants", func(t *testing.T) {
		resp, err := s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{})
		require.NoError(t, err)
		assert.Len(t, resp.Data, 5)
	})

	s.T().Run("respects limit", func(t *testing.T) {
		resp, err := s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{Limit: 2})
		require.NoError(t, err)
		assert.Len(t, resp.Data, 2)
		assert.NotEmpty(t, resp.Next, "should have next cursor")
		assert.Empty(t, resp.Prev, "should not have prev cursor on first page")
	})

	s.T().Run("orders by created_at desc by default", func(t *testing.T) {
		resp, err := s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{})
		require.NoError(t, err)
		require.Len(t, resp.Data, 5)

		// Most recent first (desc order)
		for i := 1; i < len(resp.Data); i++ {
			assert.True(t, resp.Data[i-1].CreatedAt.After(resp.Data[i].CreatedAt) ||
				resp.Data[i-1].CreatedAt.Equal(resp.Data[i].CreatedAt),
				"tenant %d should have created_at >= tenant %d", i-1, i)
		}
	})

	s.T().Run("orders by created_at asc", func(t *testing.T) {
		resp, err := s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{Order: "asc"})
		require.NoError(t, err)
		require.Len(t, resp.Data, 5)

		// Oldest first (asc order)
		for i := 1; i < len(resp.Data); i++ {
			assert.True(t, resp.Data[i-1].CreatedAt.Before(resp.Data[i].CreatedAt) ||
				resp.Data[i-1].CreatedAt.Equal(resp.Data[i].CreatedAt),
				"tenant %d should have created_at <= tenant %d", i-1, i)
		}
	})
}

func (s *ListTenantTestSuite) TestListTenantPagination() {
	// Create 25 tenants with distinct timestamps
	tenants := make([]models.Tenant, 25)
	baseTime := time.Now()
	for i := 0; i < 25; i++ {
		tenants[i] = models.Tenant{
			ID:        idgen.String(),
			CreatedAt: baseTime.Add(time.Duration(i) * time.Second),
			UpdatedAt: baseTime.Add(time.Duration(i) * time.Second),
		}
		require.NoError(s.T(), s.entityStore.UpsertTenant(s.ctx, tenants[i]))
	}

	// Wait a bit for indexing
	time.Sleep(100 * time.Millisecond)

	s.T().Run("paginate forward through all pages", func(t *testing.T) {
		var allTenants []models.Tenant
		cursor := ""
		pageCount := 0
		var firstResp, lastResp *models.ListTenantResponse

		for {
			resp, err := s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{
				Limit: 10,
				Next:  cursor,
			})
			require.NoError(t, err)
			allTenants = append(allTenants, resp.Data...)
			pageCount++
			if firstResp == nil {
				firstResp = resp
			}
			lastResp = resp

			if resp.Next == "" {
				break
			}
			cursor = resp.Next

			// Safety check to prevent infinite loop
			require.Less(t, pageCount, 10, "too many pages")
		}

		assert.Equal(t, 25, len(allTenants), "should have retrieved all tenants")
		assert.Equal(t, 3, pageCount, "should have 3 pages (10+10+5)")
		assert.Empty(t, firstResp.Prev, "first page should have no prev cursor")
		assert.Empty(t, lastResp.Next, "last page should have no next cursor")
	})

	s.T().Run("paginate backward with prev cursor", func(t *testing.T) {
		// First, traverse all the way forward collecting cursors
		var forwardIDs []string
		var cursors []string // Store Next cursors at each page
		cursor := ""

		for {
			resp, err := s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{
				Limit: 10,
				Next:  cursor,
			})
			require.NoError(t, err)
			for _, tenant := range resp.Data {
				forwardIDs = append(forwardIDs, tenant.ID)
			}

			if resp.Next == "" {
				break
			}
			cursors = append(cursors, resp.Next)
			cursor = resp.Next
		}
		require.Equal(t, 25, len(forwardIDs), "should have all tenants going forward")

		// Now traverse all the way backward from the last page
		var backwardIDs []string
		// Start from page 3 (last page), use the last Next cursor to get there
		resp, err := s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{
			Limit: 10,
			Next:  cursors[len(cursors)-1], // Go to last page
		})
		require.NoError(t, err)
		for _, tenant := range resp.Data {
			backwardIDs = append(backwardIDs, tenant.ID)
		}

		// Traverse backward
		for resp.Prev != "" {
			resp, err = s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{
				Limit: 10,
				Prev:  resp.Prev,
			})
			require.NoError(t, err)
			for _, tenant := range resp.Data {
				backwardIDs = append(backwardIDs, tenant.ID)
			}
		}

		require.Equal(t, 25, len(backwardIDs), "should have all tenants going backward")
		assert.Empty(t, resp.Prev, "first page should have no prev cursor")

		// Verify we got the same set of tenants (order differs due to page traversal order)
		forwardSet := make(map[string]bool)
		for _, id := range forwardIDs {
			forwardSet[id] = true
		}
		for _, id := range backwardIDs {
			assert.True(t, forwardSet[id], "backward traversal should contain same tenant IDs")
		}
	})
}

func (s *ListTenantTestSuite) TestListTenantExcludesDeleted() {
	// Create tenants
	tenant1 := models.Tenant{
		ID:        idgen.String(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	tenant2 := models.Tenant{
		ID:        idgen.String(),
		CreatedAt: time.Now().Add(time.Second),
		UpdatedAt: time.Now().Add(time.Second),
	}
	require.NoError(s.T(), s.entityStore.UpsertTenant(s.ctx, tenant1))
	require.NoError(s.T(), s.entityStore.UpsertTenant(s.ctx, tenant2))

	// Wait for indexing
	time.Sleep(100 * time.Millisecond)

	// List should show both
	resp, err := s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{})
	require.NoError(s.T(), err)
	assert.Len(s.T(), resp.Data, 2)

	// Delete one
	require.NoError(s.T(), s.entityStore.DeleteTenant(s.ctx, tenant1.ID))

	// Wait for index update
	time.Sleep(100 * time.Millisecond)

	// List should show only one
	resp, err = s.entityStore.ListTenant(s.ctx, models.ListTenantRequest{})
	require.NoError(s.T(), err)
	assert.Len(s.T(), resp.Data, 1)
	assert.Equal(s.T(), tenant2.ID, resp.Data[0].ID)
}
