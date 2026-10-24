package deliverymq

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/hookdeck/outpost/internal/batcher"
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
func newRetryLookup(getter RetryEventGetter, batchSize int, batchWait time.Duration) (func(context.Context, RetryTask) (*logstore.AttemptRecord, error), *attemptLookupBatcher, error) {
	if batchGetter, ok := getter.(retryAttemptBatchGetter); ok && batchSize > 1 {
		b, err := newAttemptLookupBatcher(getter, batchGetter, batchSize, batchWait)
		if err != nil {
			return nil, nil, err
		}
		return b.Lookup, b, nil
	}
	return func(ctx context.Context, task RetryTask) (*logstore.AttemptRecord, error) {
		return lookupAttempt(ctx, getter, task)
	}, nil, nil
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
// error is returned to every caller of that query, a key's read error only to
// that key's callers.
type attemptLookupBatcher struct {
	single  RetryEventGetter
	batch   retryAttemptBatchGetter
	batcher *batcher.Batcher[*lookupWaiter]
}

func newAttemptLookupBatcher(single RetryEventGetter, batch retryAttemptBatchGetter, size int, wait time.Duration) (*attemptLookupBatcher, error) {
	b := &attemptLookupBatcher{single: single, batch: batch}
	// Queries run concurrently; the scheduler's max concurrency bounds how
	// many lookups can be waiting.
	bt, err := batcher.New(batcher.Config{
		MaxItems: min(size, maxRetryLookupBatchSize),
		MaxWait:  wait,
	}, b.execute)
	if err != nil {
		return nil, err
	}
	b.batcher = bt
	return b, nil
}

// Lookup returns the latest attempt of the task, or nil when there is none.
// Tasks without an attempt time are looked up on their own.
func (b *attemptLookupBatcher) Lookup(ctx context.Context, task RetryTask) (*logstore.AttemptRecord, error) {
	if task.AttemptTime.IsZero() {
		return lookupAttempt(ctx, b.single, task)
	}
	w := &lookupWaiter{ctx: ctx, task: task, done: make(chan lookupResult, 1)}
	if err := b.batcher.Add(ctx, w); err != nil {
		return nil, errLookupBatcherClosed
	}
	select {
	case res := <-w.done:
		return res.record, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close fails the pending lookups, cancels the running queries and waits for
// them.
func (b *attemptLookupBatcher) Close() {
	b.batcher.Close()
}

// execute runs the queries of one batch. ctx is canceled when the batcher
// closes; a batch flushed by Close fails without a query.
func (b *attemptLookupBatcher) execute(ctx context.Context, batch []*lookupWaiter) {
	if ctx.Err() != nil {
		for _, w := range batch {
			w.done <- lookupResult{err: errLookupBatcherClosed}
		}
		return
	}

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
		b.executeGroup(ctx, live[start:end])
		start = end
	}
}

// queryContext keeps the values of the first caller's context (logger, trace)
// and its deadline, but not its cancellation: the query serves the whole group.
// It ends when closed is canceled.
func queryContext(first, closed context.Context) (context.Context, context.CancelFunc) {
	ctx := context.WithoutCancel(first)
	var cancel context.CancelFunc
	if deadline, ok := first.Deadline(); ok {
		ctx, cancel = context.WithDeadline(ctx, deadline)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	stop := context.AfterFunc(closed, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (b *attemptLookupBatcher) executeGroup(closed context.Context, group []*lookupWaiter) {
	ctx, cancel := queryContext(group[0].ctx, closed)
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
	var keyErrs logstore.KeyErrors
	if errors.As(err, &keyErrs) {
		err = nil
	}
	for _, w := range group {
		if err != nil {
			w.done <- lookupResult{err: err}
			continue
		}
		if keyErr, ok := keyErrs[lookupKey(w.task)]; ok {
			w.done <- lookupResult{err: keyErr}
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
