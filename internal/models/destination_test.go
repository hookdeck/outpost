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

func TestDestinationModel(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	model := models.NewDestinationModel()

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
				ID:          ids[i],
				Type:        "webhooks",
				Topics:      []string{"user.created", "user.updated"},
				Config:      map[string]string{"url": "https://example.com"},
				Credentials: map[string]string{},
				CreatedAt:   time.Now(),
				DisabledAt:  nil,
				TenantID:    tenantID,
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
			Type:        "webhooks",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{"url": "https://example.com"},
			Credentials: map[string]string{},
			DisabledAt:  nil,
			TenantID:    tenantID,
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
			assert.Equal(t, inputDestination.Credentials, destination.Credentials)
			assert.Equal(t, inputDestination.TenantID, destination.TenantID)
		}
	})
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

func TestDestination_Validate(t *testing.T) {
	t.Parallel()

	t.Run("validates valid", func(t *testing.T) {
		t.Parallel()
		destination := models.Destination{
			ID:          uuid.New().String(),
			Type:        "webhooks",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{"url": "https://example.com"},
			Credentials: map[string]string{},
			CreatedAt:   time.Now(),
			TenantID:    uuid.New().String(),
			DisabledAt:  nil,
		}
		assert.Nil(t, destination.Validate(context.Background()))
	})

	t.Run("validates invalid type", func(t *testing.T) {
		t.Parallel()
		destination := models.Destination{
			ID:          uuid.New().String(),
			Type:        "invalid",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{},
			Credentials: map[string]string{},
			CreatedAt:   time.Now(),
			TenantID:    uuid.New().String(),
			DisabledAt:  nil,
		}
		assert.ErrorContains(t, destination.Validate(context.Background()), "invalid destination type")
	})

	t.Run("validates invalid config", func(t *testing.T) {
		t.Parallel()
		destination := models.Destination{
			ID:          uuid.New().String(),
			Type:        "webhooks",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{},
			Credentials: map[string]string{},
			CreatedAt:   time.Now(),
			TenantID:    uuid.New().String(),
			DisabledAt:  nil,
		}
		assert.ErrorContains(t,
			destination.Validate(context.Background()),
			"url is required for webhook destination config",
		)
	})

	t.Run("validates invalid credentials", func(t *testing.T) {
		t.Parallel()
		destination := models.Destination{
			ID:     uuid.New().String(),
			Type:   "rabbitmq",
			Topics: []string{"user.created", "user.updated"},
			Config: map[string]string{
				"server_url": "localhost:5672",
				"exchange":   "events",
			},
			Credentials: map[string]string{
				"username":    "guest",
				"notpassword": "guest",
			},
			CreatedAt:  time.Now(),
			TenantID:   uuid.New().String(),
			DisabledAt: nil,
		}
		assert.ErrorContains(t,
			destination.Validate(context.Background()),
			"password is required for rabbitmq destination credentials",
		)
	})
}

func testCredentialsEncryption(t *testing.T, redisClient *redis.Client, cipher models.Cipher, model *models.DestinationModel) {
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

	err := model.Set(context.Background(), redisClient, input)
	require.Nil(t, err)

	actual, err := redisClient.HGetAll(context.Background(), fmt.Sprintf("tenant:%s:destination:%s", input.TenantID, input.ID)).Result()
	require.Nil(t, err)
	assert.NotEqual(t, input.Credentials, actual["credentials"])
	decryptedCredentials, err := cipher.Decrypt([]byte(actual["credentials"]))
	require.Nil(t, err)
	jsonCredentials, _ := json.Marshal(input.Credentials)
	assert.Equal(t, string(jsonCredentials), string(decryptedCredentials))
}

// Test that credentials are encrypted
func TestDestinationModel_CredentialsEncryption(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	cipher := models.NewAESCipher("secret")
	model := models.NewDestinationModel(models.DestinationModelWithCipher(cipher))

	testCredentialsEncryption(t, redisClient, cipher, model)
}

func TestDestinationModel_Options(t *testing.T) {
	t.Parallel()

	t.Run("default should use cipher with empty string secret", func(t *testing.T) {
		t.Parallel()
		model := models.NewDestinationModel()
		redisClient := testutil.CreateTestRedisClient(t)
		cipher := models.NewAESCipher("")
		testCredentialsEncryption(t, redisClient, cipher, model)
	})

	t.Run("should accept custom cipher", func(t *testing.T) {
		t.Parallel()
		cipher := models.NewAESCipher("custom")
		model := models.NewDestinationModel(models.DestinationModelWithCipher(cipher))
		redisClient := testutil.CreateTestRedisClient(t)
		testCredentialsEncryption(t, redisClient, cipher, model)
	})
}
