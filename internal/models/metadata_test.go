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

func TestMetadataRepo_DeleteManyDestination(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	metadataRepo := models.NewMetadataRepo(redisClient, models.NewAESCipher("secret"))

	t.Run("delete many", func(t *testing.T) {
		tenantID := uuid.New().String()
		ids := make([]string, 5)
		for i := 0; i < 5; i++ {
			ids[i] = uuid.New().String()
			metadataRepo.UpsertDestination(context.Background(), models.Destination{
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

		count, err := metadataRepo.DeleteManyDestination(context.Background(), tenantID, ids...)
		require.Nil(t, err)
		assert.Equal(t, int64(5), count)

		for _, id := range ids {
			actual, err := metadataRepo.RetrieveDestination(context.Background(), tenantID, id)
			require.Nil(t, err)
			require.Nil(t, actual)
		}
	})
}

func TestMetadataRepo_ListDestination(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	metadataRepo := models.NewMetadataRepo(redisClient, models.NewAESCipher("secret"))

	t.Run("returns empty", func(t *testing.T) {
		t.Parallel()
		destinations, err := metadataRepo.ListDestinationByTenant(context.Background(), uuid.New().String())
		require.Nil(t, err)
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
			metadataRepo.UpsertDestination(context.Background(), inputDestination)
		}

		destinations, err := metadataRepo.ListDestinationByTenant(context.Background(), tenantID)
		require.Nil(t, err)
		require.Len(t, destinations, 5)
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
