package mqs_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/require"
)

func TestMQ_RabbitMQRedialCooldown(t *testing.T) {
	t.Parallel()
	queue := mqs.NewRabbitMQQueue(&mqs.RabbitMQConfig{
		ServerURL: "amqp://guest:guest@127.0.0.1:1/",
		Queue:     "test",
	})
	ctx := context.Background()

	// The broker is unreachable, so the first publish dials and fails.
	firstErr := queue.Publish(ctx, &Msg{ID: "first"})
	require.Error(t, firstErr)

	// Within the cooldown the cached dial error is returned without redialing.
	require.ErrorIs(t, queue.Publish(ctx, &Msg{ID: "second"}), firstErr)
}

func TestIntegrationMQ_RabbitMQPublishReconnects(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQRabbitMQConfig(t)

	ctx := context.Background()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	receive := func(t *testing.T) *Msg {
		t.Helper()
		subscription, err := queue.Subscribe(ctx)
		require.NoError(t, err)
		defer subscription.Shutdown(ctx)
		receiveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		msg, err := subscription.Receive(receiveCtx)
		require.NoError(t, err)
		msg.Ack()
		parsed := &Msg{}
		require.NoError(t, parsed.FromMessage(msg))
		return parsed
	}

	// Sanity check: publish and receive over the initial connection.
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "before-disconnect"}))
	require.Equal(t, "before-disconnect", receive(t).ID)

	// Simulate the broker dropping the connection.
	require.NoError(t, mqs.ForceCloseRabbitMQConnection(queue))

	// The publish must transparently redial instead of failing forever.
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "after-disconnect"}))
	require.Equal(t, "after-disconnect", receive(t).ID)
}
