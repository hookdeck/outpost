package mcpevents

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEmitter records events; block, when set, holds every Emit until it is
// closed or the ctx ends.
type fakeEmitter struct {
	mu      sync.Mutex
	events  []opevents.Event
	block   chan struct{}
	entered chan struct{}
	err     error
}

func (f *fakeEmitter) Emit(ctx context.Context, ev opevents.Event) error {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return f.err
}

func (f *fakeEmitter) Events() []opevents.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]opevents.Event(nil), f.events...)
}

func newTestEmitter(t *testing.T, inner EventEmitter, mutate func(*AsyncEmitterConfig)) *AsyncEmitter {
	t.Helper()
	var cfg AsyncEmitterConfig
	if mutate != nil {
		mutate(&cfg)
	}
	e := NewAsyncEmitter(inner, cfg)
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func TestAsyncEmitter_Delivers(t *testing.T) {
	t.Parallel()
	inner := &fakeEmitter{}
	e := newTestEmitter(t, inner, nil)
	ctx, cancel := context.WithCancel(context.Background())
	for i := range 10 {
		require.NoError(t, e.Emit(ctx, opevents.Event{Topic: "mcp.subscription.expired", TenantID: string(rune('a' + i))}))
	}
	cancel() // a request ctx ending doesn't cancel queued events
	require.NoError(t, e.Close())
	assert.Len(t, inner.Events(), 10)
	assert.Zero(t, e.Dropped())
	assert.Zero(t, e.Failed())
}

func TestAsyncEmitter_Timeout(t *testing.T) {
	t.Parallel()
	inner := &fakeEmitter{block: make(chan struct{})}
	e := newTestEmitter(t, inner, func(c *AsyncEmitterConfig) { c.Timeout = 20 * time.Millisecond })
	require.NoError(t, e.Emit(context.Background(), opevents.Event{Topic: "x"}))
	require.Eventually(t, func() bool { return e.Failed() == 1 }, 5*time.Second, 5*time.Millisecond)
	assert.Empty(t, inner.Events())
}

func TestAsyncEmitter_DropsWhenFull(t *testing.T) {
	t.Parallel()
	inner := &fakeEmitter{block: make(chan struct{}), entered: make(chan struct{}, 8)}
	e := newTestEmitter(t, inner, func(c *AsyncEmitterConfig) {
		c.QueueSize = 2
		c.Workers = 1
	})
	require.NoError(t, e.Emit(context.Background(), opevents.Event{Topic: "x"}))
	<-inner.entered
	require.NoError(t, e.Emit(context.Background(), opevents.Event{Topic: "x"}))
	require.NoError(t, e.Emit(context.Background(), opevents.Event{Topic: "x"}))
	start := time.Now()
	for range 5 {
		assert.ErrorIs(t, e.Emit(context.Background(), opevents.Event{Topic: "x"}), ErrQueueFull)
	}
	assert.Less(t, time.Since(start), 100*time.Millisecond, "Emit never blocks")
	assert.Equal(t, uint64(5), e.Dropped())

	close(inner.block)
	require.NoError(t, e.Close())
	assert.Len(t, inner.Events(), 3)
	assert.ErrorIs(t, e.Emit(context.Background(), opevents.Event{Topic: "x"}), ErrQueueClosed)
	assert.Equal(t, uint64(6), e.Dropped())
}

func TestAsyncEmitter_CloseGivesUp(t *testing.T) {
	t.Parallel()
	inner := &fakeEmitter{block: make(chan struct{})}
	t.Cleanup(func() { close(inner.block) })
	e := newTestEmitter(t, inner, func(c *AsyncEmitterConfig) {
		c.Workers = 1
		c.Timeout = time.Hour
		c.DrainTimeout = 50 * time.Millisecond
	})
	for range 4 {
		require.NoError(t, e.Emit(context.Background(), opevents.Event{Topic: "x"}))
	}
	start := time.Now()
	require.NoError(t, e.Close())
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.Equal(t, uint64(3), e.Dropped())
	assert.Equal(t, uint64(1), e.Failed(), "the in-flight emit is canceled")
}

func TestAsyncEmitter_Enabled(t *testing.T) {
	t.Parallel()
	logger := testutil.CreateTestLogger(t)

	// The wrapped emitter's topic filter applies: filtered events aren't queued.
	filtered := opevents.NewEmitter(nil, "", []string{"tenant.subscription.updated"}, logger)
	e := newTestEmitter(t, filtered, nil)
	assert.True(t, e.Enabled("tenant.subscription.updated"))
	assert.False(t, e.Enabled("mcp.subscription.expired"))
	require.NoError(t, e.Emit(context.Background(), opevents.Event{Topic: "mcp.subscription.expired"}))

	noop := opevents.NewEmitter(nil, "", nil, logger)
	e = newTestEmitter(t, noop, nil)
	assert.False(t, e.Enabled("anything"))

	// An emitter without a filter accepts everything.
	e = newTestEmitter(t, &fakeEmitter{}, nil)
	assert.True(t, e.Enabled("anything"))

	var _ opevents.Emitter = e
}

func TestAsyncEmitter_NoGoroutineLeak(t *testing.T) {
	leakCheck(t)
	inner := &fakeEmitter{}
	e := NewAsyncEmitter(inner, AsyncEmitterConfig{Workers: 8})
	for range 100 {
		_ = e.Emit(context.Background(), opevents.Event{Topic: "x"})
	}
	require.NoError(t, e.Close())
	require.NoError(t, e.Close())
	assert.Len(t, inner.Events(), 100)
}

func TestWorkQueue_RecoversFromPanics(t *testing.T) {
	t.Parallel()
	var handled atomic.Int64
	q := newWorkQueue(4, 1, time.Second, zapLogger(nil), func(_ context.Context, n int) {
		if n == 0 {
			panic("boom")
		}
		handled.Add(1)
	})
	require.True(t, q.tryPush(0))
	require.True(t, q.tryPush(1))
	require.True(t, q.tryPush(2))
	q.close()
	assert.Equal(t, int64(2), handled.Load(), "the worker survived the panic")
	assert.False(t, q.tryPush(3))
	assert.ErrorIs(t, q.push(context.Background(), 3), ErrQueueClosed)
}
