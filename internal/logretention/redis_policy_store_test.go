package logretention

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisPolicyStore_RedisKey(t *testing.T) {
	client := testutil.CreateTestRedisClient(t)

	tests := []struct {
		name         string
		deploymentID string
		wantKey      string
	}{
		{
			name:         "no deployment ID",
			deploymentID: "",
			wantKey:      "outpost:log_retention_ttl",
		},
		{
			name:         "with deployment ID",
			deploymentID: "dpm_001",
			wantKey:      "dpm_001:outpost:log_retention_ttl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewRedisPolicyStore(client, tt.deploymentID)
			assert.Equal(t, tt.wantKey, store.redisKey())
		})
	}
}

func TestRedisPolicyStore_GetAppliedTTL(t *testing.T) {
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)

	t.Run("key does not exist", func(t *testing.T) {
		store := NewRedisPolicyStore(client, "get_nokey")
		ttl, err := store.GetAppliedTTL(ctx)
		require.NoError(t, err)
		assert.Equal(t, -1, ttl)
	})

	t.Run("invalid value in redis", func(t *testing.T) {
		store := NewRedisPolicyStore(client, "get_invalid")
		client.Set(ctx, store.redisKey(), "not-a-number", 0)

		_, err := store.GetAppliedTTL(ctx)
		require.Error(t, err)
	})
}

func TestRedisPolicyStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)
	store := NewRedisPolicyStore(client, "dpm_001")

	// Initially should return -1 (not found)
	ttl, err := store.GetAppliedTTL(ctx)
	require.NoError(t, err)
	assert.Equal(t, -1, ttl)

	// Set a value
	require.NoError(t, store.SetAppliedTTL(ctx, 30))

	ttl, err = store.GetAppliedTTL(ctx)
	require.NoError(t, err)
	assert.Equal(t, 30, ttl)

	// Update the value
	require.NoError(t, store.SetAppliedTTL(ctx, 7))

	ttl, err = store.GetAppliedTTL(ctx)
	require.NoError(t, err)
	assert.Equal(t, 7, ttl)
}

func TestRedisPolicyStore_DeploymentIsolation(t *testing.T) {
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)

	store1 := NewRedisPolicyStore(client, "dpm_001")
	store2 := NewRedisPolicyStore(client, "dpm_002")
	storeDefault := NewRedisPolicyStore(client, "")

	require.NoError(t, store1.SetAppliedTTL(ctx, 30))
	require.NoError(t, store2.SetAppliedTTL(ctx, 60))
	require.NoError(t, storeDefault.SetAppliedTTL(ctx, 90))

	ttl1, err := store1.GetAppliedTTL(ctx)
	require.NoError(t, err)
	assert.Equal(t, 30, ttl1)

	ttl2, err := store2.GetAppliedTTL(ctx)
	require.NoError(t, err)
	assert.Equal(t, 60, ttl2)

	ttlDefault, err := storeDefault.GetAppliedTTL(ctx)
	require.NoError(t, err)
	assert.Equal(t, 90, ttlDefault)
}
