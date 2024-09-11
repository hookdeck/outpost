package models_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestDestinationModel(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	model := models.NewDestinationModel()

	input := models.Destination{
		ID:         uuid.New().String(),
		Type:       "webhooks",
		Topics:     []string{"user.created", "user.updated"},
		Config:     map[string]string{"url": "https://example.com"},
		CreatedAt:  time.Now(),
		DisabledAt: nil,
		TenantID:   uuid.New().String(),
	}

	t.Run("gets empty", func(t *testing.T) {
		actual, err := model.Get(context.Background(), redisClient, input.ID, input.TenantID)
		assert.Nil(t, actual)
		assert.Nil(t, err)
	})

	t.Run("sets", func(t *testing.T) {
		err := model.Set(context.Background(), redisClient, input)
		assert.Nil(t, err)
	})

	t.Run("gets", func(t *testing.T) {
		actual, err := model.Get(context.Background(), redisClient, input.ID, input.TenantID)
		assert.Nil(t, err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("overrides", func(t *testing.T) {
		input.Topics = []string{"*"}

		err := model.Set(context.Background(), redisClient, input)
		assert.Nil(t, err)

		actual, err := model.Get(context.Background(), redisClient, input.ID, input.TenantID)
		assert.Nil(t, err)
		assertEqualDestination(t, input, *actual)
	})

	t.Run("clears", func(t *testing.T) {
		err := model.Clear(context.Background(), redisClient, input.ID, input.TenantID)
		assert.Nil(t, err)

		actual, err := model.Get(context.Background(), redisClient, input.ID, input.TenantID)
		assert.Nil(t, actual)
		assert.Nil(t, err)
	})
}

func TestDestinationModel_ClearMany(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	model := models.NewDestinationModel()

	t.Run("clears many", func(t *testing.T) {
		tenantID := uuid.New().String()
		ids := make([]string, 5)
		for i := 0; i < 5; i++ {
			ids[i] = uuid.New().String()
			model.Set(context.Background(), redisClient, models.Destination{
				ID:         ids[i],
				Type:       "webhooks",
				Topics:     []string{"user.created", "user.updated"},
				Config:     map[string]string{"url": "https://example.com"},
				CreatedAt:  time.Now(),
				DisabledAt: nil,
				TenantID:   tenantID,
			})
		}

		count, err := model.ClearMany(context.Background(), redisClient, tenantID, ids...)
		assert.Nil(t, err)
		assert.Equal(t, int64(5), count)

		for _, id := range ids {
			actual, err := model.Get(context.Background(), redisClient, id, tenantID)
			assert.Nil(t, actual)
			assert.Nil(t, err)
		}
	})
}

func TestDestinationModel_List(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	model := models.NewDestinationModel()

	t.Run("returns empty", func(t *testing.T) {
		t.Parallel()
		destinations, err := model.List(context.Background(), redisClient, uuid.New().String())
		assert.Nil(t, err)
		assert.Empty(t, destinations)
	})

	t.Run("returns list", func(t *testing.T) {
		tenantID := uuid.New().String()
		inputDestination := models.Destination{
			Type:       "webhooks",
			Topics:     []string{"user.created", "user.updated"},
			Config:     map[string]string{"url": "https://example.com"},
			DisabledAt: nil,
			TenantID:   tenantID,
		}

		ids := make([]string, 5)
		for i := 0; i < 5; i++ {
			ids[i] = uuid.New().String()
			inputDestination.ID = ids[i]
			inputDestination.CreatedAt = time.Now()
			model.Set(context.Background(), redisClient, inputDestination)
		}

		destinations, err := model.List(context.Background(), redisClient, tenantID)
		assert.Nil(t, err)
		assert.Len(t, destinations, 5)
		for index, destination := range destinations {
			assert.Equal(t, ids[index], destination.ID)
			assert.Equal(t, inputDestination.Type, destination.Type)
			assert.Equal(t, inputDestination.Topics, destination.Topics)
			assert.Equal(t, inputDestination.Config, destination.Config)
			assert.Equal(t, inputDestination.TenantID, destination.TenantID)
		}
	})
}

func assertEqualDestination(t *testing.T, expected, actual models.Destination) {
	assert.Equal(t, expected.ID, actual.ID)
	assert.Equal(t, expected.Type, actual.Type)
	assert.Equal(t, expected.Topics, actual.Topics)
	assert.Equal(t, expected.Config, actual.Config)
	assert.True(t, cmp.Equal(expected.CreatedAt, actual.CreatedAt))
	assert.True(t, cmp.Equal(expected.DisabledAt, actual.DisabledAt))
}

func TestDestination_Validate(t *testing.T) {
	t.Parallel()

	t.Run("validates valid", func(t *testing.T) {
		destination := models.Destination{
			ID:         uuid.New().String(),
			Type:       "webhooks",
			Topics:     []string{"user.created", "user.updated"},
			Config:     map[string]string{"url": "https://example.com"},
			CreatedAt:  time.Now(),
			TenantID:   uuid.New().String(),
			DisabledAt: nil,
		}
		assert.Nil(t, destination.Validate(context.Background()))
	})

	t.Run("validates invalid config", func(t *testing.T) {
		destination := models.Destination{
			ID:         uuid.New().String(),
			Type:       "webhooks",
			Topics:     []string{"user.created", "user.updated"},
			Config:     map[string]string{},
			CreatedAt:  time.Now(),
			TenantID:   uuid.New().String(),
			DisabledAt: nil,
		}
		assert.ErrorContains(t, destination.Validate(context.Background()), "url is required for webhook destination config")
	})

	t.Run("validates invalid type", func(t *testing.T) {
		destination := models.Destination{
			ID:         uuid.New().String(),
			Type:       "invalid",
			Topics:     []string{"user.created", "user.updated"},
			Config:     map[string]string{},
			CreatedAt:  time.Now(),
			TenantID:   uuid.New().String(),
			DisabledAt: nil,
		}
		assert.ErrorContains(t, destination.Validate(context.Background()), "invalid destination type")
	})
}
