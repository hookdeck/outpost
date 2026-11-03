// External test package: testinfra imports the destination providers, which
// import mcpevents, so tests inside package mcpevents can't use it.
package mcpevents_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents"
	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/require"
)

func TestRedisVerificationStore_RedisStack(t *testing.T) {
	t.Parallel()
	testinfra.Start(t)
	runVerificationStoreIntegration(t, testinfra.NewRedisStackConfig(t))
}

func TestRedisVerificationStore_Dragonfly(t *testing.T) {
	t.Parallel()
	testinfra.Start(t)
	runVerificationStoreIntegration(t, testinfra.NewDragonflyConfig(t))
}

func runVerificationStoreIntegration(t *testing.T, cfg *internalredis.RedisConfig) {
	for _, dep := range []string{"", "dp_test_001"} {
		t.Run("deployment="+dep, func(t *testing.T) {
			client, err := internalredis.New(context.Background(), cfg)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			require.NoError(t, client.FlushDB(context.Background()).Err())
			mcpevents.RunVerificationStoreSuite(t, client, dep, time.Sleep)
		})
	}
}
