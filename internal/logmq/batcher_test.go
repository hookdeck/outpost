package logmq

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatcher_FlushesOnCountAndDelay(t *testing.T) {
	t.Parallel()
	batches := make(chan int, 10)
	b, err := newBatcher(3, 20*time.Millisecond, func(msgs []*mqs.Message) { batches <- len(msgs) })
	require.NoError(t, err)
	t.Cleanup(b.shutdown)

	for range 3 {
		require.NoError(t, b.add(&mqs.Message{}))
	}
	assert.Equal(t, 3, <-batches)

	require.NoError(t, b.add(&mqs.Message{}))
	select {
	case n := <-batches:
		assert.Equal(t, 1, n)
	case <-time.After(5 * time.Second):
		t.Fatal("delay flush did not run")
	}
}

func TestBatcher_ShutdownFlushesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	var got atomic.Int32
	b, err := newBatcher(100, time.Hour, func(msgs []*mqs.Message) { got.Add(int32(len(msgs))) })
	require.NoError(t, err)

	require.NoError(t, b.add(&mqs.Message{}))
	require.NoError(t, b.add(&mqs.Message{}))
	b.shutdown()
	b.shutdown()

	assert.Equal(t, int32(2), got.Load())
	assert.ErrorIs(t, b.add(&mqs.Message{}), errBatcherClosed)
}

func TestBatcher_ShutdownDuringTicks(t *testing.T) {
	t.Parallel()
	for range 200 {
		b, err := newBatcher(1000, time.Millisecond, func([]*mqs.Message) {})
		require.NoError(t, err)
		time.Sleep(time.Millisecond)
		done := make(chan struct{})
		go func() { b.shutdown(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("shutdown did not return")
		}
	}
}
