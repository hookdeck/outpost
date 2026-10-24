// Package batcher groups items added one at a time into batches for a flush
// function.
package batcher

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrClosed is returned by Add after Close.
var ErrClosed = errors.New("batcher: closed")

// Config sets when a batch is flushed and how many flushes run at once.
type Config struct {
	// MaxItems flushes a batch when it holds this many items, and caps the
	// size of every batch. 0 flushes on MaxWait only.
	MaxItems int
	// MaxWait flushes a batch this long after its first item was added. Items
	// left over after a batch was taken wait MaxWait again from then.
	MaxWait time.Duration
	// MaxInFlight caps concurrent flushes. When the cap is reached, Add of an
	// item that fills a batch blocks until a flush finishes. 0 means no cap.
	MaxInFlight int
}

// Batcher collects items and hands them to flush in batches of at most
// MaxItems, at the latest MaxWait after the first item of the batch. Each
// flush runs on its own goroutine, never with the batcher's lock held.
type Batcher[T any] struct {
	cfg   Config
	flush func(context.Context, []T)

	// ctx is passed to every flush and canceled by Close.
	ctx    context.Context
	cancel context.CancelFunc
	// slots bounds concurrent flushes; nil means unbounded.
	slots chan struct{}
	wg    sync.WaitGroup

	mu      sync.Mutex
	pending []T
	timer   *time.Timer
	// gen identifies the batch the timer was armed for, so a timer that fires
	// after its batch was taken does nothing.
	gen    uint64
	closed bool
	// drained is closed once Close has flushed everything.
	drained chan struct{}
}

// New returns a running batcher that calls flush with each batch. The context
// passed to flush is canceled when Close is called; Close still flushes the
// pending items, so flush decides whether to finish the work or fail it.
func New[T any](cfg Config, flush func(context.Context, []T)) (*Batcher[T], error) {
	if flush == nil {
		return nil, errors.New("batcher: flush is required")
	}
	if cfg.MaxItems < 0 || cfg.MaxInFlight < 0 {
		return nil, errors.New("batcher: MaxItems and MaxInFlight must not be negative")
	}
	if cfg.MaxWait <= 0 {
		return nil, errors.New("batcher: MaxWait must be positive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &Batcher[T]{cfg: cfg, flush: flush, ctx: ctx, cancel: cancel, drained: make(chan struct{})}
	if cfg.MaxInFlight > 0 {
		b.slots = make(chan struct{}, cfg.MaxInFlight)
	}
	return b, nil
}

// Add queues item. When item fills a batch, Add starts its flush, first
// waiting for a free slot if MaxInFlight flushes are running. If ctx ends
// while waiting, Add returns nil and the item stays queued for the timer.
func (b *Batcher[T]) Add(ctx context.Context, item T) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrClosed
	}
	b.pending = append(b.pending, item)
	if len(b.pending) == 1 {
		b.armTimer()
	}
	full := b.cfg.MaxItems > 0 && len(b.pending) >= b.cfg.MaxItems
	b.mu.Unlock()
	if !full {
		return nil
	}

	if !b.acquire(ctx.Done()) {
		return nil
	}
	b.mu.Lock()
	if b.closed || len(b.pending) < b.cfg.MaxItems {
		// Close or another Add took the batch; leftovers flush on the timer.
		b.mu.Unlock()
		b.release()
		return nil
	}
	batch := b.take()
	b.mu.Unlock()
	go b.run(batch)
	return nil
}

// Close stops accepting items, cancels the flush context, flushes the pending
// items and waits for every flush to return. Idempotent.
func (b *Batcher[T]) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		<-b.drained
		return
	}
	b.closed = true
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.cancel()
	for len(b.pending) > 0 {
		batch := b.take()
		b.mu.Unlock()
		b.acquire(nil)
		b.run(batch)
		b.mu.Lock()
	}
	b.mu.Unlock()
	b.wg.Wait()
	close(b.drained)
}

// onTimer flushes the batch the timer was armed for, if it is still pending.
func (b *Batcher[T]) onTimer(gen uint64) {
	b.acquire(nil)
	b.mu.Lock()
	if b.closed || gen != b.gen || len(b.pending) == 0 {
		b.mu.Unlock()
		b.release()
		return
	}
	batch := b.take()
	b.mu.Unlock()
	b.run(batch)
}

// armTimer starts the MaxWait timer for the current batch. Caller holds mu.
func (b *Batcher[T]) armTimer() {
	gen := b.gen
	b.timer = time.AfterFunc(b.cfg.MaxWait, func() { b.onTimer(gen) })
}

// take removes the next batch from pending and registers its flush. Caller
// holds mu and must call run with the batch while holding a slot.
func (b *Batcher[T]) take() []T {
	n := len(b.pending)
	if b.cfg.MaxItems > 0 {
		n = min(n, b.cfg.MaxItems)
	}
	batch := b.pending[:n:n]
	b.pending = b.pending[n:]
	if len(b.pending) == 0 {
		b.pending = nil
	}
	b.gen++
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	if len(b.pending) > 0 && !b.closed {
		b.armTimer()
	}
	b.wg.Add(1)
	return batch
}

// run flushes batch and frees its slot.
func (b *Batcher[T]) run(batch []T) {
	defer b.wg.Done()
	defer b.release()
	b.flush(b.ctx, batch)
}

// acquire takes a flush slot, or reports false if done closes first. A nil
// done waits until a slot is free.
func (b *Batcher[T]) acquire(done <-chan struct{}) bool {
	if b.slots == nil {
		return true
	}
	select {
	case b.slots <- struct{}{}:
		return true
	case <-done:
		return false
	}
}

func (b *Batcher[T]) release() {
	if b.slots != nil {
		<-b.slots
	}
}
