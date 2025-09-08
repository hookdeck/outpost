package app

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetInstallationAtomic(t *testing.T) {
	// This test verifies that the Installation ID generation is atomic
	// and handles concurrent access correctly

	ctx := context.Background()
	redisConfig := testutil.CreateTestRedisConfig(t)
	redisClient, err := redis.NewForTest(ctx, redisConfig)
	require.NoError(t, err)

	// Clear any existing installation ID
	redisClient.Del(ctx, outpostrcKey)

	config := telemetry.TelemetryConfig{Disabled: false}

	// Test 1: First call should create installation ID
	id1, err := getInstallation(ctx, redisClient, config)
	require.NoError(t, err)
	assert.NotEmpty(t, id1)

	// Test 2: Second call should return the same ID (atomic consistency)
	id2, err := getInstallation(ctx, redisClient, config)
	require.NoError(t, err)
	assert.Equal(t, id1, id2, "Installation ID should be consistent across calls")

	// Test 3: Verify the ID is actually stored in Redis
	storedID, err := redisClient.HGet(ctx, outpostrcKey, installationKey).Result()
	require.NoError(t, err)
	assert.Equal(t, id1, storedID, "Stored ID should match returned ID")

	// Test 4: Test with telemetry disabled
	disabledConfig := telemetry.TelemetryConfig{Disabled: true}
	id3, err := getInstallation(ctx, redisClient, disabledConfig)
	require.NoError(t, err)
	assert.Empty(t, id3, "Should return empty string when telemetry is disabled")
}

func TestGetInstallationConcurrency(t *testing.T) {
	// This test simulates concurrent access to verify atomicity
	ctx := context.Background()
	redisConfig := testutil.CreateTestRedisConfig(t)
	redisClient, err := redis.NewForTest(ctx, redisConfig)
	require.NoError(t, err)

	// Clear any existing installation ID
	redisClient.Del(ctx, outpostrcKey)

	config := telemetry.TelemetryConfig{Disabled: false}

	// Run multiple goroutines concurrently
	const numGoroutines = 10
	resultChan := make(chan string, numGoroutines)
	errorChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			id, err := getInstallation(ctx, redisClient, config)
			if err != nil {
				errorChan <- err
				return
			}
			resultChan <- id
		}()
	}

	// Collect results
	var results []string
	for i := 0; i < numGoroutines; i++ {
		select {
		case result := <-resultChan:
			results = append(results, result)
		case err := <-errorChan:
			t.Fatalf("Concurrent execution failed: %v", err)
		}
	}

	// All results should be identical (atomic behavior)
	require.Len(t, results, numGoroutines)
	expectedID := results[0]
	for i, id := range results {
		assert.Equal(t, expectedID, id, "Result %d should match first result", i)
	}

	// Verify only one ID was created in Redis
	storedID, err := redisClient.HGet(ctx, outpostrcKey, installationKey).Result()
	require.NoError(t, err)
	assert.Equal(t, expectedID, storedID)
}
