package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowlistExempt(t *testing.T) {
	t.Parallel()
	parse := func(entries ...string) *netguard.Allowlist {
		a, _, err := netguard.ParseAllowlist(entries)
		require.NoError(t, err)
		return a
	}

	loopback := allowlistExempt(parse("127.0.0.0/8", "::1/128"))
	assert.True(t, loopback("127.0.0.1"))
	assert.True(t, loopback("::1"), "IPv6 literals arrive unbracketed")
	assert.True(t, loopback("localhost"))
	assert.True(t, loopback("LOCALHOST."))
	assert.False(t, loopback("10.0.0.1"))
	assert.False(t, loopback("example.com"), "host names are never exempt")

	v4Only := allowlistExempt(parse("127.0.0.1"))
	assert.True(t, v4Only("127.0.0.1"))
	assert.False(t, v4Only("localhost"), "localhost may resolve to ::1, which isn't allowlisted")

	none := allowlistExempt(parse())
	assert.False(t, none("127.0.0.1"))
	assert.False(t, none("localhost"))
}

func TestDeliveryProcessingTTL(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{DeliveryTimeoutSeconds: 5}
	assert.Equal(t, 15*time.Second, deliveryProcessingTTL(cfg), "mcp attempts get 10s whatever the global timeout")
	cfg.DeliveryTimeoutSeconds = 30
	assert.Equal(t, 35*time.Second, deliveryProcessingTTL(cfg))
}

type parkCall struct {
	tenantID, destinationID, member string
	max                             int
	expireAt                        time.Time
}

type fakeParkStore struct {
	result tenantstore.ParkResult
	err    error
	calls  []parkCall
}

func (f *fakeParkStore) ParkRetry(_ context.Context, tenantID, destinationID, member string, max int, expireAt time.Time) (tenantstore.ParkResult, error) {
	f.calls = append(f.calls, parkCall{tenantID, destinationID, member, max, expireAt})
	return f.result, f.err
}

func TestRetryParker(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	task := deliverymq.RetryTask{EventID: "evt_1", TenantID: "t1", DestinationID: "sub_1", DestinationCreatedAt: 42}
	member, err := task.ToString()
	require.NoError(t, err)

	for _, tt := range []struct {
		result tenantstore.ParkResult
		parked bool
	}{
		{tenantstore.ParkResultParked, true},
		{tenantstore.ParkResultFull, true}, // dropped, not redelivered
		{tenantstore.ParkResultEnabled, false},
		{tenantstore.ParkResultGone, false},
	} {
		t.Run(string(tt.result), func(t *testing.T) {
			store := &fakeParkStore{result: tt.result}
			p := newRetryParker(store, testutil.CreateTestLogger(t))
			p.now = func() time.Time { return now }
			parked, err := p.ParkRetry(context.Background(), task, &models.Destination{ID: "sub_1", TenantID: "t1"})
			require.NoError(t, err)
			assert.Equal(t, tt.parked, parked)
			require.Len(t, store.calls, 1)
			assert.Equal(t, parkCall{"t1", "sub_1", member, parkedRetryMax, now.Add(parkedRetryTTL)}, store.calls[0])
		})
	}

	t.Run("expires with the destination", func(t *testing.T) {
		store := &fakeParkStore{result: tenantstore.ParkResultParked}
		p := newRetryParker(store, testutil.CreateTestLogger(t))
		expiresAt := now.Add(time.Hour)
		_, err := p.ParkRetry(context.Background(), task, &models.Destination{ExpiresAt: &expiresAt})
		require.NoError(t, err)
		assert.Equal(t, expiresAt, store.calls[0].expireAt)
	})

	t.Run("store error", func(t *testing.T) {
		boom := errors.New("boom")
		p := newRetryParker(&fakeParkStore{err: boom}, testutil.CreateTestLogger(t))
		_, err := p.ParkRetry(context.Background(), task, &models.Destination{})
		assert.ErrorIs(t, err, boom)
	})
}

type scheduledTask struct {
	task  string
	delay time.Duration
	id    string
}

type fakeScheduler struct {
	mu      sync.Mutex
	tasks   []scheduledTask
	failOn  string
	blockCh chan struct{}
}

func (f *fakeScheduler) Schedule(ctx context.Context, task string, delay time.Duration, opts ...scheduler.ScheduleOption) error {
	if f.blockCh != nil {
		select {
		case <-f.blockCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	o := &scheduler.ScheduleOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if f.failOn != "" && strings.Contains(task, f.failOn) {
		return errors.New("schedule failed")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks = append(f.tasks, scheduledTask{task, delay, o.ID})
	return nil
}

func (f *fakeScheduler) snapshot() []scheduledTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]scheduledTask(nil), f.tasks...)
}

// parkAndReenable parks n retries on a disabled mcp destination of a mem
// store, re-enables it with a refresh-style conditional write and returns
// the resume key.
func parkAndReenable(t *testing.T, store tenantstore.TenantStore, tenantID, destID string, n int, extra ...string) string {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: tenantID, CreatedAt: time.Now(), UpdatedAt: time.Now()}))
	created := time.Now().Truncate(time.Millisecond)
	dest := models.Destination{
		ID: destID, TenantID: tenantID, Type: models.DestinationTypeMCP,
		Topics: models.Topics{"order.created"}, Config: models.Config{"url": "https://example.com/"},
		Credentials: models.Credentials{"secret": "x"}, CreatedAt: created, UpdatedAt: created,
	}
	require.NoError(t, store.CreateDestination(ctx, dest))
	changed, err := store.DisableDestination(ctx, tenantID, destID, time.Now())
	require.NoError(t, err)
	require.True(t, changed)

	parker := newRetryParker(store, testutil.CreateTestLogger(t))
	for i := range n {
		task := deliverymq.RetryTask{EventID: fmt.Sprintf("evt_%03d", i), TenantID: tenantID, DestinationID: destID, DestinationCreatedAt: created.UnixMilli()}
		parked, err := parker.ParkRetry(ctx, task, &dest)
		require.NoError(t, err)
		require.True(t, parked)
	}
	for _, member := range extra {
		res, err := store.ParkRetry(ctx, tenantID, destID, member, parkedRetryMax, time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.Equal(t, tenantstore.ParkResultParked, res)
	}

	result, err := store.UpdateDestinationIfLive(ctx, dest, created, tenantstore.WithResumeParkedRetries())
	require.NoError(t, err)
	require.True(t, result.WasDisabled)
	require.NotEmpty(t, result.ResumeKey)
	return result.ResumeKey
}

func TestParkedRetryResumer_SchedulesSpreadRetries(t *testing.T) {
	t.Parallel()
	store := tenantstore.NewMemTenantStore()
	foreign := `{"EventID":"evt_x","TenantID":"other","DestinationID":"sub_1"}`
	key := parkAndReenable(t, store, "t1", "sub_1", 25, foreign, "not json")

	sched := &fakeScheduler{}
	r := newParkedRetryResumer(store, sched, testutil.CreateTestLogger(t))
	ctx, cancel := context.WithCancel(context.Background())
	r.Resume(ctx, "t1", "sub_1", key)
	cancel() // the resume runs on its own context
	r.Close()

	tasks := sched.snapshot()
	require.Len(t, tasks, 25, "invalid and foreign members are skipped")
	perDelay := map[time.Duration]int{}
	for _, task := range tasks {
		var rt deliverymq.RetryTask
		require.NoError(t, rt.FromString(task.task))
		assert.Equal(t, "sub_1", rt.DestinationID)
		assert.NotZero(t, rt.DestinationCreatedAt, "resumed retries stay generation-checked")
		assert.Equal(t, models.RetryID(rt.EventID, "sub_1")+":r", task.id)
		perDelay[task.delay]++
	}
	assert.Equal(t, map[time.Duration]int{0: 10, time.Second: 10, 2 * time.Second: 5}, perDelay,
		"at most 10 per second, in whole seconds")

	members, err := store.PopResumeMembers(context.Background(), "t1", key, 10)
	require.NoError(t, err)
	assert.Empty(t, members, "the resume set is drained and deleted")
}

func TestParkedRetryResumer_ScheduleFailureContinues(t *testing.T) {
	t.Parallel()
	store := tenantstore.NewMemTenantStore()
	key := parkAndReenable(t, store, "t1", "sub_1", 5)

	sched := &fakeScheduler{failOn: "evt_002"}
	r := newParkedRetryResumer(store, sched, testutil.CreateTestLogger(t))
	r.Resume(context.Background(), "t1", "sub_1", key)
	r.Close()
	assert.Len(t, sched.snapshot(), 4)
}

func TestParkedRetryResumer_CloseBounded(t *testing.T) {
	t.Parallel()
	store := tenantstore.NewMemTenantStore()
	key := parkAndReenable(t, store, "t1", "sub_1", 3)

	sched := &fakeScheduler{blockCh: make(chan struct{})}
	r := newParkedRetryResumer(store, sched, testutil.CreateTestLogger(t))
	r.drainTimeout = 50 * time.Millisecond
	r.Resume(context.Background(), "t1", "sub_1", key)

	done := make(chan struct{})
	go func() {
		r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after its drain timeout")
	}
	assert.Empty(t, sched.snapshot())

	// After Close, Resume is a no-op instead of a panic on the closed queue.
	r.Resume(context.Background(), "t1", "sub_1", key)
}

func TestParkedRetryResumer_QueueFullDrops(t *testing.T) {
	t.Parallel()
	sched := &fakeScheduler{blockCh: make(chan struct{})}
	store := &countingResumeStore{}
	r := newParkedRetryResumer(store, sched, testutil.CreateTestLogger(t))
	defer func() {
		close(sched.blockCh)
		r.Close()
	}()
	// Workers block on the first member of their job; the queue holds the
	// rest; one more is dropped without blocking.
	for i := range resumeWorkers + resumeQueueSize + 1 {
		r.Resume(context.Background(), "t1", "sub_1", fmt.Sprintf("key_%d", i))
	}
	assert.Eventually(t, func() bool { return store.pops() == resumeWorkers }, 5*time.Second, 10*time.Millisecond)
}

// countingResumeStore returns one member per key once.
type countingResumeStore struct {
	mu     sync.Mutex
	popped map[string]bool
	n      int
}

func (s *countingResumeStore) PopResumeMembers(_ context.Context, tenantID, key string, n int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.popped == nil {
		s.popped = map[string]bool{}
	}
	if s.popped[key] {
		return nil, nil
	}
	s.popped[key] = true
	s.n++
	task := deliverymq.RetryTask{EventID: "evt_" + key, TenantID: tenantID, DestinationID: "sub_1"}
	member, _ := task.ToString()
	return []string{member}, nil
}

func (s *countingResumeStore) DeleteResumeSet(context.Context, string, string) error { return nil }

func (s *countingResumeStore) pops() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

func TestDestinationDisabler(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := tenantstore.NewMemTenantStore()
	require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: "t1", CreatedAt: time.Now(), UpdatedAt: time.Now()}))
	dest := models.Destination{
		ID: "d1", TenantID: "t1", Type: "webhook", Topics: models.Topics{"*"},
		Config: models.Config{"url": "https://example.com/"}, Credentials: models.Credentials{"secret": "x"},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, store.CreateDestination(ctx, dest))

	d := newDestinationDisabler(store)
	changed, err := d.DisableDestinationIfEnabled(ctx, "t1", "d1")
	require.NoError(t, err)
	assert.True(t, changed)
	got, err := store.RetrieveDestination(ctx, "t1", "d1")
	require.NoError(t, err)
	require.NotNil(t, got.DisabledAt)

	changed, err = d.DisableDestinationIfEnabled(ctx, "t1", "d1")
	require.NoError(t, err)
	assert.False(t, changed, "already disabled")

	require.NoError(t, store.DeleteDestination(ctx, "t1", "d1"))
	changed, err = d.DisableDestinationIfEnabled(ctx, "t1", "d1")
	require.NoError(t, err, "a deleted destination is a no-op")
	assert.False(t, changed)
	_, err = store.RetrieveDestination(ctx, "t1", "d1")
	assert.ErrorIs(t, err, tenantstore.ErrDestinationDeleted, "never resurrected")

	changed, err = d.DisableDestinationIfEnabled(ctx, "t1", "missing")
	require.NoError(t, err)
	assert.False(t, changed)
	require.NoError(t, d.DisableDestination(ctx, "t1", "missing"))
}

func TestTenantStoreConfig_MCP(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.InitDefaults()
	cfg.MaxMCPSubscriptionsPerTenant = 7
	got := tenantStoreConfig(cfg, nil)
	assert.Equal(t, map[string]int{"mcp": 7}, got.TypeLimits)
	assert.Equal(t, []string{"mcp"}, got.IndexedTypes)
	assert.Equal(t, cfg.MaxDestinationsPerTenant, got.MaxDestinationsPerTenant)
}
