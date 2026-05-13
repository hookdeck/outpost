package opevents_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMQSink_Send(t *testing.T) {
	t.Parallel()

	t.Run("publishes event to queue", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		queue := mqs.NewInMemoryQueue(&mqs.InMemoryConfig{Name: "opevents-test-send"})
		sink := opevents.NewMQSink(queue)

		require.NoError(t, sink.Init(ctx))
		defer sink.Close()

		// Subscribe BEFORE sending — in-memory queue drops messages with no subscribers
		sub, err := queue.Subscribe(ctx)
		require.NoError(t, err)
		defer sub.Shutdown(ctx)

		event := testEvent()
		require.NoError(t, sink.Send(ctx, event))

		recvCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		msg, err := sub.Receive(recvCtx)
		require.NoError(t, err)

		msg.Ack()

		var got opevents.OperatorEvent
		require.NoError(t, json.Unmarshal(msg.Body, &got))
		assert.Equal(t, event.ID, got.ID)
		assert.Equal(t, event.Topic, got.Topic)
		assert.Equal(t, event.TenantID, got.TenantID)
	})
}
