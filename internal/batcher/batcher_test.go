package batcher_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/batcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder collects flushed batches.
type recorder struct {
	mu      sync.Mutex
	batches [][]int
	ctxErrs []error
}

func (r *recorder) flush(ctx context.Context, items []int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches = append(r.batches, append([]int(nil), items...))
	r.ctxErrs = append(r.ctxErrs, ctx.Err())
}

func (r *recorder) sizes() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	sizes := make([]int, len(r.batches))
	for i, b := range r.batches {
		sizes[i] = len(b)
	}
	return sizes
}

func TestNew_Validates(t *testing.T) {
	t.Parallel()
	noop := func(context.Context, []int) {}
	_, err := batcher.New[int](batcher.Config{MaxWait: time.Second}, nil)
	assert.Error(t, err)
	_, err = batcher.New(batcher.Config{}, noop)
	assert.Error(t, err)
	_, err = batcher.New(batcher.Config{MaxItems: -1, MaxWait: time.Second}, noop)
	assert.Error(t, err)
	_, err = batcher.New(batcher.Config{MaxInFlight: -1, MaxWait: time.Second}, noop)
	assert.Error(t, err)
}

func TestBatcher_FlushesAtMaxItems(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var r recorder
		b, err := batcher.New(batcher.Config{MaxItems: 3, MaxWait: time.Hour}, r.flush)
		require.NoError(t, err)
		defer b.Close()

		for i := range 7 {
			require.NoError(t, b.Add(t.Context(), i))
		}
		synctest.Wait()
		assert.Equal(t, []int{3, 3}, r.sizes())
	})
}

func TestBatcher_FlushesMaxWaitAfterFirstItem(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var r recorder
		b, err := batcher.New(batcher.Config{MaxItems: 100, MaxWait: time.Second}, r.flush)
		require.NoError(t, err)
		defer b.Close()

		require.NoError(t, b.Add(t.Context(), 1))
		time.Sleep(600 * time.Millisecond)
		require.NoError(t, b.Add(t.Context(), 2))
		time.Sleep(399 * time.Millisecond)
		synctest.Wait()
		assert.Empty(t, r.sizes(), "flushed before MaxWait")

		time.Sleep(time.Millisecond)
		synctest.Wait()
		assert.Equal(t, []int{2}, r.sizes())

		// Idle: nothing pending, nothing flushed.
		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, []int{2}, r.sizes())
	})
}

// Adds racing past MaxItems must not turn the overflow into a small batch of
// its own: it waits for more items or the timer.
func TestBatcher_OverflowWaitsForFullBatch(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var r recorder
		b, err := batcher.New(batcher.Config{MaxItems: 2, MaxWait: time.Second, MaxInFlight: 1}, func(ctx context.Context, items []int) {
			<-release
			r.flush(ctx, items)
		})
		require.NoError(t, err)

		require.NoError(t, b.Add(t.Context(), 0))
		require.NoError(t, b.Add(t.Context(), 1)) // batch 1 flushing, holds the slot
		synctest.Wait()
		for i := 2; i < 5; i++ {
			go func() { assert.NoError(t, b.Add(t.Context(), i)) }() // 3 items, each Add of a full batch waits
		}
		synctest.Wait()
		close(release)
		synctest.Wait()
		assert.Equal(t, []int{2, 2}, r.sizes(), "the fifth item is not flushed alone")

		time.Sleep(time.Second)
		synctest.Wait()
		assert.Equal(t, []int{2, 2, 1}, r.sizes())
		b.Close()
	})
}

func TestBatcher_ConcurrentCloseWaitsForDrain(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var flushed atomic.Int32
		b, err := batcher.New(batcher.Config{MaxItems: 1, MaxWait: time.Hour, MaxInFlight: 1}, func(context.Context, []int) {
			time.Sleep(time.Second)
			flushed.Add(1)
		})
		require.NoError(t, err)
		require.NoError(t, b.Add(t.Context(), 1))
		synctest.Wait()

		var wg sync.WaitGroup
		for range 3 {
			wg.Go(func() {
				b.Close()
				assert.Equal(t, int32(1), flushed.Load(), "Close returned before the drain")
			})
		}
		wg.Wait()
	})
}

func TestBatcher_MaxItemsZeroFlushesOnTimeOnly(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var r recorder
		b, err := batcher.New(batcher.Config{MaxWait: time.Second}, r.flush)
		require.NoError(t, err)
		defer b.Close()

		for i := range 500 {
			require.NoError(t, b.Add(t.Context(), i))
		}
		time.Sleep(time.Second)
		synctest.Wait()
		assert.Equal(t, []int{500}, r.sizes())
	})
}

func TestBatcher_BatchesNeverExceedMaxItems(t *testing.T) {
	t.Parallel()
	var r recorder
	b, err := batcher.New(batcher.Config{MaxItems: 10, MaxWait: time.Millisecond, MaxInFlight: 1}, func(ctx context.Context, items []int) {
		time.Sleep(time.Millisecond)
		r.flush(ctx, items)
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for g := range 20 {
		wg.Go(func() {
			for i := range 100 {
				assert.NoError(t, b.Add(context.Background(), g*100+i))
			}
		})
	}
	wg.Wait()
	b.Close()

	total := 0
	for _, n := range r.sizes() {
		assert.LessOrEqual(t, n, 10)
		total += n
	}
	assert.Equal(t, 2000, total)
}

func TestBatcher_MaxInFlightBlocksFillingAdd(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var running, maxRunning atomic.Int32
		b, err := batcher.New(batcher.Config{MaxItems: 2, MaxWait: time.Hour, MaxInFlight: 1}, func(context.Context, []int) {
			n := running.Add(1)
			for {
				m := maxRunning.Load()
				if n <= m || maxRunning.CompareAndSwap(m, n) {
					break
				}
			}
			<-release
			running.Add(-1)
		})
		require.NoError(t, err)

		require.NoError(t, b.Add(t.Context(), 1))
		require.NoError(t, b.Add(t.Context(), 2)) // starts flush 1, which blocks
		synctest.Wait()

		// Items that do not fill a batch never block.
		require.NoError(t, b.Add(t.Context(), 3))

		var returned atomic.Bool
		go func() {
			assert.NoError(t, b.Add(t.Context(), 4)) // fills batch 2, waits for the slot
			returned.Store(true)
		}()
		synctest.Wait()
		assert.False(t, returned.Load(), "Add returned while the only slot was taken")

		release <- struct{}{}
		synctest.Wait()
		assert.True(t, returned.Load())

		close(release)
		b.Close()
		assert.Equal(t, int32(1), maxRunning.Load())
	})
}

func TestBatcher_AddWaitEndsWithContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var r recorder
		b, err := batcher.New(batcher.Config{MaxItems: 1, MaxWait: time.Second, MaxInFlight: 1}, func(ctx context.Context, items []int) {
			<-release
			r.flush(ctx, items)
		})
		require.NoError(t, err)

		require.NoError(t, b.Add(t.Context(), 1)) // holds the slot
		synctest.Wait()

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error)
		go func() { done <- b.Add(ctx, 2) }()
		synctest.Wait()
		cancel()
		assert.NoError(t, <-done, "item stays queued, so Add reports success")

		close(release)
		time.Sleep(time.Second) // the item flushes on the timer
		synctest.Wait()
		assert.Equal(t, []int{1, 1}, r.sizes())
		b.Close()
	})
}

func TestBatcher_UnboundedFlushesRunConcurrently(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var running atomic.Int32
		b, err := batcher.New(batcher.Config{MaxItems: 1, MaxWait: time.Hour}, func(context.Context, []int) {
			running.Add(1)
			<-release
		})
		require.NoError(t, err)

		for i := range 5 {
			require.NoError(t, b.Add(t.Context(), i))
		}
		synctest.Wait()
		assert.Equal(t, int32(5), running.Load())
		close(release)
		b.Close()
	})
}

func TestBatcher_CloseFlushesPendingWithCanceledContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var r recorder
		b, err := batcher.New(batcher.Config{MaxItems: 2, MaxWait: time.Hour, MaxInFlight: 1}, r.flush)
		require.NoError(t, err)

		for i := range 5 {
			require.NoError(t, b.Add(t.Context(), i))
		}
		b.Close()
		b.Close()

		assert.Equal(t, []int{2, 2, 1}, r.sizes())
		assert.ErrorIs(t, r.ctxErrs[2], context.Canceled, "Close flush runs with the canceled context")
		assert.ErrorIs(t, b.Add(t.Context(), 9), batcher.ErrClosed)
	})
}

func TestBatcher_CloseWaitsForInFlightFlush(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var finished atomic.Bool
		b, err := batcher.New(batcher.Config{MaxItems: 1, MaxWait: time.Hour}, func(ctx context.Context, _ []int) {
			<-ctx.Done()
			time.Sleep(time.Second)
			finished.Store(true)
		})
		require.NoError(t, err)

		require.NoError(t, b.Add(t.Context(), 1))
		synctest.Wait()
		b.Close()
		assert.True(t, finished.Load())
	})
}

// Close racing timer flushes must return; the batcher this replaces could
// deadlock here.
func TestBatcher_CloseDuringTimerFlushes(t *testing.T) {
	t.Parallel()
	for range 200 {
		b, err := batcher.New(batcher.Config{MaxItems: 1000, MaxWait: time.Millisecond, MaxInFlight: 1}, func(context.Context, []int) {})
		require.NoError(t, err)
		for i := range 5 {
			require.NoError(t, b.Add(context.Background(), i))
			time.Sleep(200 * time.Microsecond)
		}
		done := make(chan struct{})
		go func() { b.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not return")
		}
	}
}
