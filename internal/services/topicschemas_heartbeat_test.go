package services_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopicSchemasHeartbeatWorker(t *testing.T) {
	rdb := testutil.CreateTestRedisClient(t)
	w := services.NewTopicSchemasHeartbeatWorker(rdb, "dp_1", "abc", 20*time.Millisecond, time.Minute, testutil.CreateTestLogger(t))
	assert.Equal(t, "topic-schemas-heartbeat", w.Name())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Reported at once, then kept alive, under the deployment's key.
	require.Eventually(t, func() bool {
		live, err := topicschema.IsLive(context.Background(), rdb, "dp_1", "abc")
		return err == nil && live
	}, time.Second, 5*time.Millisecond)
	ttl, err := rdb.TTL(context.Background(), topicschema.LiveKey("dp_1", "abc")).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, 50*time.Second)
	live, err := topicschema.IsLive(context.Background(), rdb, "", "abc")
	require.NoError(t, err)
	assert.False(t, live, "other deployments don't see it")

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run didn't return after the context ended")
	}
}
