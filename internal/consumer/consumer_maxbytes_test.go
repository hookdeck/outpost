package consumer_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMaxBytesQueue(t *testing.T) mqs.Queue {
	t.Helper()
	mq := mqs.NewQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}})
	cleanup, err := mq.Init(context.Background())
	require.NoError(t, err)
	t.Cleanup(cleanup)
	return mq
}

// A byte limit far above the traffic leaves the count limit in charge.
func TestConsumer_MaxBytesAboveTrafficReachesConcurrency(t *testing.T) {
	t.Parallel()

	const concurrency = 5
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mq := newMaxBytesQueue(t)
	subscription, err := mq.Subscribe(ctx, mqs.WithConcurrency(concurrency), mqs.WithMaxBytes(1<<30))
	require.NoError(t, err)

	var (
		mu      sync.Mutex
		running int
		peak    int
		handled int
	)
	full := make(chan struct{})
	handler := &handlerImpl{handle: func(_ context.Context, msg *mqs.Message) error {
		mu.Lock()
		running++
		peak = max(peak, running)
		if running == concurrency {
			select {
			case <-full:
			default:
				close(full)
			}
		}
		mu.Unlock()

		select {
		case <-full:
		case <-ctx.Done():
		}
		time.Sleep(50 * time.Millisecond)

		mu.Lock()
		running--
		handled++
		if handled == 2*concurrency {
			cancel()
		}
		mu.Unlock()
		msg.Ack()
		return nil
	}}

	for i := range 2 * concurrency {
		require.NoError(t, mq.Publish(ctx, &Message{ID: fmt.Sprint(i)}))
	}

	csm := consumer.New(subscription, handler, consumer.WithConcurrency(concurrency))
	require.ErrorIs(t, csm.Run(ctx), context.Canceled)

	assert.Equal(t, 2*concurrency, handled)
	assert.Equal(t, concurrency, peak)
}
