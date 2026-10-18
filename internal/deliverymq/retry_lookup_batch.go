package deliverymq

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/scheduler"
)

const (
	// maxRetryLookupBatchSize caps the lookups per batched query.
	maxRetryLookupBatchSize = 100

	// retryLookupBatchSpan is the widest span of attempt times in one batched
	// query; one old task would otherwise widen the range read for all others.
	retryLookupBatchSpan = 15 * time.Minute
)

var errLookupBatcherClosed = errors.New("retry lookup batcher closed")

// retryAttemptBatchGetter is implemented by log stores that return the latest
// attempt of many keys in one query.
type retryAttemptBatchGetter interface {
	ListLatestAttempts(ctx context.Context, lookups []logstore.LatestAttemptLookup) (map[logstore.AttemptKey]*logstore.AttemptRecord, error)
}

// lookupAttempt returns the latest attempt of the task, or nil when there is none.
func lookupAttempt(ctx context.Context, getter RetryEventGetter, task RetryTask) (*logstore.AttemptRecord, error) {
	resp, err := getter.ListAttempt(ctx, task.attemptLookupRequest())
	if err != nil || len(resp.Data) == 0 {
		return nil, err
	}
	return resp.Data[0], nil
}

// effectiveLookupBatchSize caps the batch size at the execution concurrency
// when one is set: no more lookups can be waiting than tasks can execute, so a
// larger batch would only ever flush on the wait timer.
func effectiveLookupBatchSize(size, maxConcurrency int) int {
	if maxConcurrency > 0 && maxConcurrency < size {
		return maxConcurrency
	}
	return size
}

// newRetryLookup returns the lookup the retry executor uses, with its batcher
// when batching is enabled and the getter supports it.
func newRetryLookup(getter RetryEventGetter, batchSize int, batchWait time.Duration) (func(context.Context, RetryTask) (*logstore.AttemptRecord, error), *attemptLookupBatcher) {
	if batchGetter, ok := getter.(retryAttemptBatchGetter); ok && batchSize > 1 {
		batcher := newAttemptLookupBatcher(getter, batchGetter, batchSize, batchWait)
		return batcher.Lookup, batcher
	}
	return func(ctx context.Context, task RetryTask) (*logstore.AttemptRecord, error) {
		return lookupAttempt(ctx, getter, task)
	}, nil
}

type lookupResult struct {
	record *logstore.AttemptRecord
	err    error
}

type lookupWaiter struct {
	ctx  context.Context
	task RetryTask
	done chan lookupResult // buffered: the flush never blocks on a caller that left
}

// attemptLookupBatcher coalesces the attempt lookups of concurrently executing
// retries into one log store query. It flushes when size lookups are pending or
// wait has passed since the first one. Each caller gets its own result; a query
// error is returned to every caller of that query.
type attemptLookupBatcher struct {
	single RetryEventGetter
	batch  retryAttemptBatchGetter
	size   int
	wait   time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	pending []*lookupWaiter
	timer   *time.Timer
	gen     uint64
	closed  bool
}

func newAttemptLookupBatcher(single RetryEventGetter, batch retryAttemptBatchGetter, size int, wait time.Duration) *attemptLookupBatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &attemptLookupBatcher{
		single: single,
		batch:  batch,
		size:   min(size, maxRetryLookupBatchSize),
		wait:   wait,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Lookup returns the latest attempt of the task, or nil when there is none.
// Tasks without an attempt time are looked up on their own.
func (b *attemptLookupBatcher) Lookup(ctx context.Context, task RetryTask) (*logstore.AttemptRecord, error) {
	if task.AttemptTime.IsZero() {
		return lookupAttempt(ctx, b.single, task)
	}
	w := &lookupWaiter{ctx: ctx, task: task, done: make(chan lookupResult, 1)}
	b.add(w)
	select {
	case res := <-w.done:
		return res.record, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *attemptLookupBatcher) add(w *lookupWaiter) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		w.done <- lookupResult{err: errLookupBatcherClosed}
		return
	}
	b.pending = append(b.pending, w)
	if len(b.pending) >= b.size {
		batch := b.take()
		b.mu.Unlock()
		go b.execute(batch)
		return
	}
	if len(b.pending) == 1 {
		gen := b.gen
		b.timer = time.AfterFunc(b.wait, func() { b.flush(gen) })
	}
	b.mu.Unlock()
}

func (b *attemptLookupBatcher) flush(gen uint64) {
	b.mu.Lock()
	if gen != b.gen || len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.take()
	b.mu.Unlock()
	b.execute(batch)
}

// take empties the pending list; the caller holds mu and must call execute.
func (b *attemptLookupBatcher) take() []*lookupWaiter {
	batch := b.pending
	b.pending = nil
	b.gen++
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.wg.Add(1)
	return batch
}

// Close fails the pending lookups, cancels the running query and waits for it.
func (b *attemptLookupBatcher) Close() {
	b.mu.Lock()
	b.closed = true
	var batch []*lookupWaiter
	if len(b.pending) > 0 {
		batch = b.take()
	}
	b.mu.Unlock()
	b.cancel()
	if batch != nil {
		for _, w := range batch {
			w.done <- lookupResult{err: errLookupBatcherClosed}
		}
		b.wg.Done()
	}
	b.wg.Wait()
}

func (b *attemptLookupBatcher) execute(batch []*lookupWaiter) {
	defer b.wg.Done()

	live := batch[:0]
	for _, w := range batch {
		if w.ctx.Err() == nil {
			live = append(live, w)
		}
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].task.AttemptTime.Before(live[j].task.AttemptTime) })
	for start := 0; start < len(live); {
		end := start + 1
		for end < len(live) && live[end].task.AttemptTime.Sub(live[start].task.AttemptTime) <= retryLookupBatchSpan {
			end++
		}
		b.executeGroup(live[start:end])
		start = end
	}
}

// queryContext keeps the values of the first caller's context (logger, trace)
// and its deadline, but not its cancellation: the query serves the whole group.
// It ends when the batcher closes.
func (b *attemptLookupBatcher) queryContext(first context.Context) (context.Context, context.CancelFunc) {
	ctx := context.WithoutCancel(first)
	var cancel context.CancelFunc
	if deadline, ok := first.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, deadline)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	stop := context.AfterFunc(b.ctx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (b *attemptLookupBatcher) executeGroup(group []*lookupWaiter) {
	ctx, cancel := b.queryContext(group[0].ctx)
	defer cancel()
	if len(group) == 1 {
		record, err := lookupAttempt(ctx, b.single, group[0].task)
		group[0].done <- lookupResult{record: record, err: err}
		return
	}

	notBefore := make(map[logstore.AttemptKey]time.Time, len(group))
	for _, w := range group {
		key := lookupKey(w.task)
		bound := w.task.AttemptTime.Add(-retryLookupSkew)
		if prev, ok := notBefore[key]; !ok || bound.Before(prev) {
			notBefore[key] = bound
		}
	}
	lookups := make([]logstore.LatestAttemptLookup, 0, len(notBefore))
	for key, bound := range notBefore {
		lookups = append(lookups, logstore.LatestAttemptLookup{Key: key, NotBefore: bound})
	}

	found, err := b.batch.ListLatestAttempts(ctx, lookups)
	for _, w := range group {
		if err != nil {
			w.done <- lookupResult{err: err}
			continue
		}
		record := found[lookupKey(w.task)]
		if record != nil && record.Attempt.Time.Before(w.task.AttemptTime.Add(-retryLookupSkew)) {
			record = nil
		}
		w.done <- lookupResult{record: record}
	}
}

func lookupKey(task RetryTask) logstore.AttemptKey {
	return logstore.AttemptKey{TenantID: task.TenantID, EventID: task.EventID, DestinationID: task.DestinationID}
}

// batchingScheduler stops the batcher with the scheduler.
type batchingScheduler struct {
	scheduler.Scheduler
	batcher *attemptLookupBatcher
}

func (s batchingScheduler) Shutdown() error {
	s.batcher.Close()
	return s.Scheduler.Shutdown()
}
