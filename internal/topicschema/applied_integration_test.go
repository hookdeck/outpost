package topicschema_test

import (
	"context"
	"testing"

	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/require"
)

func TestApply_RedisStack(t *testing.T) {
	t.Parallel()
	testinfra.Start(t)
	runApplyIntegration(t, testinfra.NewRedisStackConfig(t))
}

func TestApply_Dragonfly(t *testing.T) {
	t.Parallel()
	testinfra.Start(t)
	runApplyIntegration(t, testinfra.NewDragonflyConfig(t))
}

func runApplyIntegration(t *testing.T, cfg *internalredis.RedisConfig) {
	client, err := internalredis.New(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.FlushDB(context.Background()).Err())
	topicschema.RunApplySuiteForTest(t, client)
}
