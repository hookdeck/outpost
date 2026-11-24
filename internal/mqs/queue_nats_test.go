package mqs_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMQ_NATSUnreachableServerFailsFast(t *testing.T) {
	t.Parallel()
	// Nothing listens on this port, so the connect attempt inside Publish
	// must surface an error rather than hang or panic.
	queue := mqs.NewNATSQueue(&mqs.NATSConfig{
		ServerURL: "nats://127.0.0.1:1",
		Subject:   "test",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := queue.Publish(ctx, &Msg{ID: "first"})
	require.Error(t, err)
}

func TestIntegrationMQ_NATSLoggableIDPerMessage(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQNATSConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	subscription, err := queue.Subscribe(ctx)
	require.NoError(t, err)
	defer subscription.Shutdown(ctx)

	require.NoError(t, queue.Publish(ctx, &Msg{ID: "first"}))
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "second"}))

	first, err := subscription.Receive(ctx)
	require.NoError(t, err)
	first.Ack()
	second, err := subscription.Receive(ctx)
	require.NoError(t, err)
	second.Ack()

	assert.NotEmpty(t, first.LoggableID)
	assert.NotEqual(t, first.LoggableID, second.LoggableID)
}
