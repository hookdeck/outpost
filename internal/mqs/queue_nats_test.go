package mqs_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNATSQueue_Defaults(t *testing.T) {
	t.Parallel()

	t.Run("applies defaults when unset", func(t *testing.T) {
		t.Parallel()
		queue := mqs.NewNATSQueue(&mqs.NATSConfig{ServerURL: "nats://127.0.0.1:1"})
		assert.Equal(t, 5, mqs.NATSQueueConfig(queue).MaxDeliver)
		assert.Equal(t, 60*time.Second, mqs.NATSQueueConfig(queue).AckWait)
	})

	t.Run("preserves explicit values", func(t *testing.T) {
		t.Parallel()
		queue := mqs.NewNATSQueue(&mqs.NATSConfig{
			ServerURL:  "nats://127.0.0.1:1",
			MaxDeliver: 10,
			AckWait:    5 * time.Second,
		})
		assert.Equal(t, 10, mqs.NATSQueueConfig(queue).MaxDeliver)
		assert.Equal(t, 5*time.Second, mqs.NATSQueueConfig(queue).AckWait)
	})
}

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
