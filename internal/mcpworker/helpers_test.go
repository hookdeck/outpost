package mcpworker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	topicA = "order.created"
	topicB = "order.shipped"

	schemaV1 = `{"type":"object","properties":{"id":{"type":"string"},"total":{"type":"number"}}}`
	// schemaV2 removes total from schemaV1.
	schemaV2 = `{"type":"object","properties":{"id":{"type":"string"}}}`
)

var (
	testKey      = bytes.Repeat([]byte{7}, 32)
	testSecret   = "whsec_" + base64.StdEncoding.EncodeToString(testKey)
	testPrevKey  = bytes.Repeat([]byte{9}, 32)
	testPrevSecr = "whsec_" + base64.StdEncoding.EncodeToString(testPrevKey)
)

func snapshotOf(schemas map[string]string) topicschema.Snapshot {
	s := topicschema.Snapshot{Topics: map[string]topicschema.SnapshotTopic{}}
	for name, schema := range schemas {
		s.Topics[name] = topicschema.SnapshotTopic{MCPEnabled: true, PayloadSchema: json.RawMessage(schema)}
	}
	return s
}

// localSnapshot is the default configuration: topicA and topicB on schemaV1.
func localSnapshot() topicschema.Snapshot {
	return snapshotOf(map[string]string{topicA: schemaV1, topicB: schemaV1})
}

type fakeNotifier struct {
	mu    sync.Mutex
	terms []mcpevents.Termination
	err   error
	// gate, when set, makes EnqueueWait wait for it (or ctx).
	gate chan struct{}
}

func (n *fakeNotifier) EnqueueWait(ctx context.Context, t mcpevents.Termination) error {
	if n.gate != nil {
		select {
		case <-n.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return n.err
	}
	n.terms = append(n.terms, t)
	return nil
}

func (n *fakeNotifier) list() []mcpevents.Termination {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]mcpevents.Termination(nil), n.terms...)
}

type fakeEmitter struct {
	mu     sync.Mutex
	events []opevents.Event
	topics map[string]bool // nil: every topic enabled
}

func (e *fakeEmitter) Emit(_ context.Context, ev opevents.Event) error {
	if !e.Enabled(ev.Topic) {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
	return nil
}

func (e *fakeEmitter) Enabled(topic string) bool {
	return e.topics == nil || e.topics[topic]
}

func (e *fakeEmitter) byTopic(topic string) []opevents.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []opevents.Event
	for _, ev := range e.events {
		if ev.Topic == topic {
			out = append(out, ev)
		}
	}
	return out
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// countingStore counts calls and can inject failures.
type countingStore struct {
	tenantstore.TenantStore
	retrieves, scans, lists atomic.Int64
	deleteErr               func(destinationID string) error
	retrieveHook            func(destinationID string)
	listHook                func(ctx context.Context)
}

func (s *countingStore) RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*models.Destination, error) {
	s.retrieves.Add(1)
	if s.retrieveHook != nil {
		s.retrieveHook(destinationID)
	}
	return s.TenantStore.RetrieveDestination(ctx, tenantID, destinationID)
}

func (s *countingStore) DeleteDestinationIf(ctx context.Context, tenantID, destinationID string, c tenantstore.DeleteCondition) (tenantstore.DeleteResult, error) {
	if s.deleteErr != nil {
		if err := s.deleteErr(destinationID); err != nil {
			return tenantstore.DeleteResult{}, err
		}
	}
	return s.TenantStore.DeleteDestinationIf(ctx, tenantID, destinationID, c)
}

func (s *countingStore) ListIndexedDestinations(ctx context.Context, typ, topic string, maxScore int64, limit int) ([]tenantstore.IndexedDestination, error) {
	s.lists.Add(1)
	if s.listHook != nil {
		s.listHook(ctx)
	}
	return s.TenantStore.ListIndexedDestinations(ctx, typ, topic, maxScore, limit)
}

func (s *countingStore) ScanIndexedDestinations(ctx context.Context, typ, topic string, cursor uint64, count int) ([]tenantstore.IndexedDestination, uint64, error) {
	s.scans.Add(1)
	return s.TenantStore.ScanIndexedDestinations(ctx, typ, topic, cursor, count)
}

type harness struct {
	mr       *miniredis.Miniredis // nil on a real server
	rdb      internalredis.Client
	store    *countingStore
	notifier *fakeNotifier
	emitter  *fakeEmitter
	clock    *testClock
	snapshot topicschema.Snapshot
	cfg      Config
	w        *Worker
}

func testLogger(t *testing.T) *logging.Logger {
	return logging.NewTestLogger(zaptest.NewLogger(t))
}

// newHarness builds a worker on the in-memory tenant store (indexing mcp)
// and miniredis, with the local configuration localSnapshot.
func newHarness(t *testing.T, opts ...func(*Config)) *harness {
	t.Helper()
	return newHarnessWithStore(t, func(internalredis.Client) tenantstore.TenantStore {
		return tenantstore.NewMemTenantStore(tenantstore.MemWithIndexedTypes(models.DestinationTypeMCP))
	}, opts...)
}

// newHarnessWithStore builds the tenant store with newStore, given the
// worker's Redis client (miniredis).
func newHarnessWithStore(t *testing.T, newStore func(internalredis.Client) tenantstore.TenantStore, opts ...func(*Config)) *harness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return newHarnessOn(t, mr, rdb, newStore, opts...)
}

// newHarnessOn builds a harness on rdb; mr is nil for a real server.
func newHarnessOn(t *testing.T, mr *miniredis.Miniredis, rdb internalredis.Client, newStore func(internalredis.Client) tenantstore.TenantStore, opts ...func(*Config)) *harness {
	t.Helper()
	h := &harness{
		mr:       mr,
		rdb:      rdb,
		store:    &countingStore{TenantStore: newStore(rdb)},
		notifier: &fakeNotifier{},
		emitter:  &fakeEmitter{},
		clock:    &testClock{now: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)},
		snapshot: localSnapshot(),
	}
	h.cfg = Config{
		Redis:    rdb,
		Store:    h.store,
		Snapshot: h.snapshot,
		Notifier: h.notifier,
		Emitter:  h.emitter,
		Logger:   testLogger(t),
		Now:      h.clock.Now,
	}
	for _, opt := range opts {
		opt(&h.cfg)
	}
	h.snapshot = h.cfg.Snapshot
	w, err := New(h.cfg)
	require.NoError(t, err)
	h.w = w
	return h
}

func ptr[T any](v T) *T { return &v }

// sub stores an mcp subscription to topic created an hour ago, against the
// local schema of the topic.
func (h *harness) sub(t *testing.T, tenantID, id, topic string, expiresAt *time.Time, opts ...func(*models.Destination)) models.Destination {
	t.Helper()
	ctx := context.Background()
	if tenant, err := h.store.RetrieveTenant(ctx, tenantID); err == nil && tenant == nil {
		require.NoError(t, h.store.UpsertTenant(ctx, testutil.TenantFactory.Any(testutil.TenantFactory.WithID(tenantID))))
	}
	created := h.clock.Now().Add(-time.Hour).Truncate(time.Millisecond)
	d := models.Destination{
		ID:       id,
		TenantID: tenantID,
		Type:     models.DestinationTypeMCP,
		Topics:   models.Topics{topic},
		Config: models.Config{
			"url":             "https://receiver.example.com/" + id,
			"subscription_id": id,
			"principal":       "user_" + id,
			"event":           topic,
			"arguments":       "{}",
			"schema_hash":     h.snapshot.TopicHash(topic),
		},
		Credentials: models.Credentials{"secret": testSecret},
		CreatedAt:   created,
		UpdatedAt:   created,
	}
	if expiresAt != nil {
		e := expiresAt.Truncate(time.Millisecond)
		d.ExpiresAt = &e
	}
	for _, opt := range opts {
		opt(&d)
	}
	require.NoError(t, h.store.UpsertDestination(ctx, d))
	return d
}

func (h *harness) webhook(t *testing.T, tenantID, id, topic string) {
	t.Helper()
	require.NoError(t, h.store.UpsertDestination(context.Background(), testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID(id),
		testutil.DestinationFactory.WithTenantID(tenantID),
		testutil.DestinationFactory.WithTopics([]string{topic}),
	)))
}

// apply records s as the applied configuration.
func (h *harness) apply(t *testing.T, s topicschema.Snapshot, allowBreaking bool) {
	t.Helper()
	_, err := topicschema.Apply(context.Background(), h.rdb, h.cfg.DeploymentID, s, topicschema.ApplyOptions{
		AllowBreaking: allowBreaking,
		Now:           h.clock.Now,
	})
	require.NoError(t, err)
}

func (h *harness) live(t *testing.T, tenantID, id string) bool {
	t.Helper()
	d, err := h.store.RetrieveDestination(context.Background(), tenantID, id)
	if err != nil {
		require.ErrorIs(t, err, tenantstore.ErrDestinationDeleted)
		return false
	}
	return d != nil
}

// indexed lists the members of an index (topic "" for the global one) with
// their scores.
func (h *harness) indexed(t *testing.T, topic string) map[string]int64 {
	t.Helper()
	refs, err := h.store.TenantStore.ListIndexedDestinations(context.Background(), models.DestinationTypeMCP, topic, tenantstore.NoExpiryScore, 10_000)
	require.NoError(t, err)
	out := make(map[string]int64, len(refs))
	for _, r := range refs {
		out[r.DestinationID] = r.Score
	}
	return out
}

func (h *harness) pass(t *testing.T) PassStats {
	t.Helper()
	return h.w.pass(context.Background())
}
