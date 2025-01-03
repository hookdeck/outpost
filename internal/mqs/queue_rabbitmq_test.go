package mqs_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationRabbitMQ(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	t.Run("should route messages to correct queue", func(t *testing.T) {
		ctx := context.Background()
		serverURL := testinfra.EnsureRabbitMQ()
		exchange := "test-exchange"

		// Create and declare infrastructure for both queues
		config1 := &mqs.QueueConfig{
			RabbitMQ: &mqs.RabbitMQConfig{
				ServerURL: serverURL,
				Exchange:  exchange,
				Queue:     "test-queue-1",
			},
		}
		infra1 := mqinfra.New(config1)
		require.NoError(t, infra1.Declare(ctx))
		defer func() {
			require.NoError(t, infra1.TearDown(ctx))
		}()

		config2 := &mqs.QueueConfig{
			RabbitMQ: &mqs.RabbitMQConfig{
				ServerURL: serverURL,
				Exchange:  exchange,
				Queue:     "test-queue-2",
			},
		}
		infra2 := mqinfra.New(config2)
		require.NoError(t, infra2.Declare(ctx))
		defer func() {
			require.NoError(t, infra2.TearDown(ctx))
		}()

		// Create queues
		queue1 := mqs.NewQueue(config1)
		cleanup1, err := queue1.Init(ctx)
		require.NoError(t, err)
		defer cleanup1()

		queue2 := mqs.NewQueue(config2)
		cleanup2, err := queue2.Init(ctx)
		require.NoError(t, err)
		defer cleanup2()

		// Subscribe to both queues
		sub1, err := queue1.Subscribe(ctx)
		require.NoError(t, err)
		defer sub1.Shutdown(ctx)

		sub2, err := queue2.Subscribe(ctx)
		require.NoError(t, err)
		defer sub2.Shutdown(ctx)

		// Publish messages to both queues
		msg1 := &testutil.MockMsg{ID: uuid.New().String()}
		err = queue1.Publish(ctx, msg1)
		assert.NoError(t, err, "failed to publish to queue1")

		msg2 := &testutil.MockMsg{ID: uuid.New().String()}
		err = queue2.Publish(ctx, msg2)
		assert.NoError(t, err, "failed to publish to queue2")

		// Helper to receive all messages from a subscription with timeout
		receiveAllMessages := func(sub mqs.Subscription, timeout time.Duration) []*testutil.MockMsg {
			var messages []*testutil.MockMsg
			for {
				// Create a context with timeout for each receive attempt
				receiveCtx, cancel := context.WithTimeout(ctx, timeout)
				received, err := sub.Receive(receiveCtx)
				cancel()

				if err != nil {
					// If we timeout, assume no more messages
					break
				}

				parsed := &testutil.MockMsg{}
				err = parsed.FromMessage(received)
				require.NoError(t, err)
				messages = append(messages, parsed)
				received.Ack()
			}
			return messages
		}

		// Receive all messages from both queues
		messages1 := receiveAllMessages(sub1, 100*time.Millisecond)
		messages2 := receiveAllMessages(sub2, 100*time.Millisecond)

		// Check queue1
		if assert.Len(t, messages1, 1, "queue1 should have exactly 1 message") {
			assert.Equal(t, msg1.ID, messages1[0].ID, "queue1 should have msg1")
		}

		// Check queue2
		if assert.Len(t, messages2, 1, "queue2 should have exactly 1 message") {
			assert.Equal(t, msg2.ID, messages2[0].ID, "queue2 should have msg2")
		}
	})
}
