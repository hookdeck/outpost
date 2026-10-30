package deliverymq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLookupStore struct {
	mu          sync.Mutex
	singleCalls []logstore.ListAttemptRequest
	batchCalls  [][]logstore.LatestAttemptLookup
	records     map[logstore.AttemptKey]*logstore.AttemptRecord
	keyErrs     map[logstore.AttemptKey]error
	err         error
	block       chan struct{}
}

func (f *fakeLookupStore) record(eventID string, at time.Time) {
	if f.records == nil {
		f.records = map[logstore.AttemptKey]*logstore.AttemptRecord{}
	}
	f.records[logstore.AttemptKey{TenantID: "t", EventID: eventID, DestinationID: "d"}] = &logstore.AttemptRecord{
		Attempt: &models.Attempt{ID: "att_" + eventID, EventID: eventID, Time: at},
		Event:   &models.Event{ID: eventID},
	}
}

func (f *fakeLookupStore) ListAttempt(ctx context.Context, req logstore.ListAttemptRequest) (logstore.ListAttemptResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.singleCalls = append(f.singleCalls, req)
	if f.err != nil {
		return logstore.ListAttemptResponse{}, f.err
	}
	if r, ok := f.records[logstore.AttemptKey{TenantID: req.TenantIDs[0], EventID: req.EventIDs[0], DestinationID: req.DestinationIDs[0]}]; ok {
		return logstore.ListAttemptResponse{Data: []*logstore.AttemptRecord{r}}, nil
	}
	return logstore.ListAttemptResponse{}, nil
}

func (f *fakeLookupStore) ListLatestAttempts(ctx context.Context, lookups []logstore.LatestAttemptLookup) (map[logstore.AttemptKey]*logstore.AttemptRecord, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batchCalls = append(f.batchCalls, lookups)
	if f.err != nil {
		return nil, f.err
	}
	out := map[logstore.AttemptKey]*logstore.AttemptRecord{}
	var keyErrs logstore.KeyErrors
	for _, l := range lookups {
		if err, ok := f.keyErrs[l.Key]; ok {
			if keyErrs == nil {
				keyErrs = logstore.KeyErrors{}
			}
			keyErrs[l.Key] = err
			continue
		}
		if r, ok := f.records[l.Key]; ok && !r.Attempt.Time.Before(l.NotBefore) {
			out[l.Key] = r
		}
	}
	if keyErrs != nil {
		return out, keyErrs
	}
	return out, nil
}

func (f *fakeLookupStore) counts() (single, batch int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.singleCalls), len(f.batchCalls)
}

func lookupTask(eventID string, at time.Time) RetryTask {
	return RetryTask{TenantID: "t", EventID: eventID, DestinationID: "d", AttemptTime: at}
}

type lookupOutcome struct {
	record *logstore.AttemptRecord
	err    error
}

// lookupAll runs the lookups concurrently and returns their outcomes in order.
func lookupAll(ctx context.Context, b *attemptLookupBatcher, tasks ...RetryTask) []lookupOutcome {
	out := make([]lookupOutcome, len(tasks))
	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i].record, out[i].err = b.Lookup(ctx, task)
		}()
	}
	wg.Wait()
	return out
}

func TestLookupBatcher_FlushesAtSize(t *testing.T) {
	t.Parallel()
	store := &fakeLookupStore{}
	now := time.Now()
	for i := range 3 {
		store.record(fmt.Sprintf("e%d", i), now)
	}
	b := newAttemptLookupBatcher(store, store, 3, time.Minute)
	defer b.Close()

	out := lookupAll(context.Background(), b, lookupTask("e0", now), lookupTask("e1", now), lookupTask("e2", now))

	for i, o := range out {
		require.NoError(t, o.err)
		assert.Equal(t, fmt.Sprintf("att_e%d", i), o.record.Attempt.ID)
	}
	single, batch := store.counts()
	assert.Equal(t, 0, single)
	assert.Equal(t, 1, batch)
	assert.Len(t, store.batchCalls[0], 3)
}

func TestLookupBatcher_FlushesAfterWait(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		store := &fakeLookupStore{}
		now := time.Now()
		store.record("e0", now)
		store.record("e1", now)
		b := newAttemptLookupBatcher(store, store, 50, 30*time.Millisecond)
		defer b.Close()

		var out []lookupOutcome
		done := make(chan struct{})
		go func() {
			out = lookupAll(context.Background(), b, lookupTask("e0", now), lookupTask("e1", now))
			close(done)
		}()

		time.Sleep(29 * time.Millisecond)
		synctest.Wait()
		_, batch := store.counts()
		assert.Equal(t, 0, batch, "nothing is sent before the wait passes")

		time.Sleep(time.Millisecond)
		<-done
		assert.NoError(t, out[0].err)
		assert.NoError(t, out[1].err)
		single, batch := store.counts()
		assert.Equal(t, 0, single)
		assert.Equal(t, 1, batch)
	})
}

func TestLookupBatcher_BoundIsCarriedTimeMinusSkew(t *testing.T) {
	t.Parallel()
	store := &fakeLookupStore{}
	now := time.Now()
	b := newAttemptLookupBatcher(store, store, 2, time.Minute)
	defer b.Close()

	lookupAll(context.Background(), b, lookupTask("e0", now), lookupTask("e1", now.Add(time.Minute)))

	require.Len(t, store.batchCalls, 1)
	for _, l := range store.batchCalls[0] {
		want := now.Add(-retryLookupSkew)
		if l.Key.EventID == "e1" {
			want = now.Add(time.Minute - retryLookupSkew)
		}
		assert.True(t, l.NotBefore.Equal(want), l.Key.EventID)
	}
}

func TestLookupBatcher_SplitsBySpan(t *testing.T) {
	t.Parallel()
	store := &fakeLookupStore{}
	now := time.Now()
	b := newAttemptLookupBatcher(store, store, 5, time.Minute)
	defer b.Close()

	lookupAll(context.Background(), b,
		lookupTask("e0", now),
		lookupTask("e1", now.Add(time.Minute)),
		lookupTask("e2", now.Add(retryLookupBatchSpan+time.Minute)),
		lookupTask("e3", now.Add(retryLookupBatchSpan+2*time.Minute)),
		lookupTask("e4", now.Add(2*time.Hour)),
	)

	single, batch := store.counts()
	assert.Equal(t, 2, batch)
	assert.Equal(t, 1, single, "a group of one uses the single lookup")
	assert.Len(t, store.batchCalls[0], 2)
	assert.Len(t, store.batchCalls[1], 2)
}

func TestLookupBatcher_BatchOfOneUsesSingleLookup(t *testing.T) {
	t.Parallel()
	store := &fakeLookupStore{}
	now := time.Now()
	store.record("e0", now)
	b := newAttemptLookupBatcher(store, store, 50, 10*time.Millisecond)
	defer b.Close()

	rec, err := b.Lookup(context.Background(), lookupTask("e0", now))

	require.NoError(t, err)
	assert.Equal(t, "att_e0", rec.Attempt.ID)
	single, batch := store.counts()
	assert.Equal(t, 1, single)
	assert.Equal(t, 0, batch)
	assert.NotNil(t, store.singleCalls[0].TimeFilter.GTE)
}

func TestLookupBatcher_TaskWithoutAttemptTimeIsNotBatched(t *testing.T) {
	t.Parallel()
	store := &fakeLookupStore{}
	store.record("e0", time.Now())
	b := newAttemptLookupBatcher(store, store, 50, time.Minute)
	defer b.Close()

	rec, err := b.Lookup(context.Background(), lookupTask("e0", time.Time{}))

	require.NoError(t, err)
	assert.NotNil(t, rec)
	single, batch := store.counts()
	assert.Equal(t, 1, single)
	assert.Equal(t, 0, batch)
}

func TestLookupBatcher_MissingKeyIsNoRecord(t *testing.T) {
	t.Parallel()
	store := &fakeLookupStore{}
	now := time.Now()
	store.record("e0", now)
	b := newAttemptLookupBatcher(store, store, 2, time.Minute)
	defer b.Close()

	out := lookupAll(context.Background(), b, lookupTask("e0", now), lookupTask("missing", now))

	assert.NoError(t, out[0].err)
	assert.NotNil(t, out[0].record)
	assert.NoError(t, out[1].err)
	assert.Nil(t, out[1].record)
}

func TestLookupBatcher_RecordOlderThanOwnBoundIsNoRecord(t *testing.T) {
	t.Parallel()
	store := &fakeLookupStore{}
	now := time.Now()
	store.record("e0", now.Add(-time.Hour))
	store.record("e1", now)
	b := newAttemptLookupBatcher(store, store, 3, time.Minute)
	defer b.Close()

	// the same key carried at two times: the older bound finds the record, the
	// newer caller must not take it
	out := lookupAll(context.Background(), b,
		lookupTask("e0", now.Add(-time.Hour)), lookupTask("e0", now), lookupTask("e1", now))

	got := map[bool]int{}
	for _, o := range out[:2] {
		got[o.record != nil]++
	}
	assert.Equal(t, 1, got[true])
	assert.Equal(t, 1, got[false])
}

func TestLookupBatcher_ErrorFailsEveryCaller(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	store := &fakeLookupStore{err: boom}
	now := time.Now()
	b := newAttemptLookupBatcher(store, store, 3, time.Minute)
	defer b.Close()

	out := lookupAll(context.Background(), b, lookupTask("e0", now), lookupTask("e1", now), lookupTask("e2", now))

	for _, o := range out {
		assert.ErrorIs(t, o.err, boom)
		assert.Nil(t, o.record)
	}
}

func TestLookupBatcher_KeyErrorFailsOnlyThatKey(t *testing.T) {
	t.Parallel()
	bad := errors.New("bad row")
	store := &fakeLookupStore{keyErrs: map[logstore.AttemptKey]error{{TenantID: "t", EventID: "e1", DestinationID: "d"}: bad}}
	now := time.Now()
	store.record("e0", now)
	store.record("e2", now)
	b := newAttemptLookupBatcher(store, store, 3, time.Minute)
	defer b.Close()

	out := lookupAll(context.Background(), b, lookupTask("e0", now), lookupTask("e1", now), lookupTask("e2", now))

	require.NoError(t, out[0].err)
	assert.Equal(t, "att_e0", out[0].record.Attempt.ID)
	assert.ErrorIs(t, out[1].err, bad)
	assert.Nil(t, out[1].record)
	require.NoError(t, out[2].err)
	assert.Equal(t, "att_e2", out[2].record.Attempt.ID)
}

func TestLookupBatcher_CallerCancelDoesNotAffectOthers(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		store := &fakeLookupStore{}
		now := time.Now()
		store.record("e0", now)
		store.record("e1", now)
		b := newAttemptLookupBatcher(store, store, 50, 100*time.Millisecond)
		defer b.Close()

		cancelCtx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		var canceled, other lookupOutcome
		wg.Add(2)
		go func() {
			defer wg.Done()
			canceled.record, canceled.err = b.Lookup(cancelCtx, lookupTask("e0", now))
		}()
		go func() {
			defer wg.Done()
			other.record, other.err = b.Lookup(context.Background(), lookupTask("e1", now))
		}()
		synctest.Wait()
		cancel()
		wg.Wait()

		assert.ErrorIs(t, canceled.err, context.Canceled)
		require.NoError(t, other.err)
		assert.Equal(t, "att_e1", other.record.Attempt.ID)
	})
}

func TestLookupBatcher_CloseFailsPendingAndStopsQueries(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		store := &fakeLookupStore{block: make(chan struct{})}
		now := time.Now()
		b := newAttemptLookupBatcher(store, store, 2, time.Minute)

		// one batch in flight (blocked in the store), one lookup pending
		inflight := make(chan []lookupOutcome, 1)
		go func() { inflight <- lookupAll(context.Background(), b, lookupTask("e0", now), lookupTask("e1", now)) }()
		synctest.Wait()
		pending := make(chan lookupOutcome, 1)
		go func() {
			rec, err := b.Lookup(context.Background(), lookupTask("e2", now))
			pending <- lookupOutcome{rec, err}
		}()
		synctest.Wait()

		b.Close()

		for _, o := range <-inflight {
			assert.Error(t, o.err)
		}
		assert.ErrorIs(t, (<-pending).err, errLookupBatcherClosed)
		_, err := b.Lookup(context.Background(), lookupTask("e3", now))
		assert.ErrorIs(t, err, errLookupBatcherClosed)
	})
}

func TestLookupBatcher_QueryKeepsFirstCallerValuesAndDeadline(t *testing.T) {
	t.Parallel()
	type ctxKey struct{}
	store := &ctxCapturingStore{fakeLookupStore: &fakeLookupStore{}}
	now := time.Now()
	b := newAttemptLookupBatcher(store, store, 2, time.Minute)
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), ctxKey{}, "v"), time.Hour)
	defer cancel()
	lookupAll(ctx, b, lookupTask("e0", now), lookupTask("e1", now))

	require.NotNil(t, store.ctx)
	assert.Equal(t, "v", store.ctx.Value(ctxKey{}))
	_, hasDeadline := store.ctx.Deadline()
	assert.True(t, hasDeadline)
}

type ctxCapturingStore struct {
	*fakeLookupStore
	ctx context.Context
}

func (s *ctxCapturingStore) ListLatestAttempts(ctx context.Context, lookups []logstore.LatestAttemptLookup) (map[logstore.AttemptKey]*logstore.AttemptRecord, error) {
	s.ctx = ctx
	return s.fakeLookupStore.ListLatestAttempts(ctx, lookups)
}

func TestNewRetryLookup_BatchingEnabledOnlyWhenConfiguredAndSupported(t *testing.T) {
	t.Parallel()
	store := &fakeLookupStore{}
	for _, size := range []int{0, 1} {
		_, batcher := newRetryLookup(store, size, time.Millisecond)
		assert.Nil(t, batcher, "size %d", size)
	}
	_, batcher := newRetryLookup(store, 2, time.Millisecond)
	require.NotNil(t, batcher)
	batcher.Close()

	_, batcher = newRetryLookup(singleOnlyGetter{store}, 50, time.Millisecond)
	assert.Nil(t, batcher)
}

func TestEffectiveLookupBatchSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		size, maxConcurrency, want int
	}{
		{50, 0, 50},
		{50, 100, 50},
		{50, 50, 50},
		{50, 10, 10},
		{50, 1, 1},
		{0, 10, 0},
		{1, 10, 1},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, effectiveLookupBatchSize(tt.size, tt.maxConcurrency), "size %d, concurrency %d", tt.size, tt.maxConcurrency)
	}
}

type singleOnlyGetter struct{ s *fakeLookupStore }

func (g singleOnlyGetter) ListAttempt(ctx context.Context, req logstore.ListAttemptRequest) (logstore.ListAttemptResponse, error) {
	return g.s.ListAttempt(ctx, req)
}
