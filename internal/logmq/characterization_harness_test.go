package logmq_test

// Characterization suite for the logmq post-persist pipeline.
//
// These tests pin the CURRENT behavior of BatchProcessor.processBatch wired to
// the REAL alert evaluator, REAL in-memory log store, REAL opevents emitter and a
// miniredis-backed alert store. The only doubles are at the external boundary:
//   - recordingSink     (opevents.Sink): records emitted operator events, can
//     inject Send failures.
//   - recordingDisabler (logmq.DestinationDisabler): records disable calls.
//   - countingMessage   (mqs.QueueMessage): counts ack/nack for exactly-once.
//
// Observable oracles (assert ONLY on these):
//   - sink.records   (ordered list of {topic, destID, attemptID})
//   - per-message ack/nack counters (exactly-once terminal state)
//   - logStore.ListAttempt / ListEvent
//
// Entries process concurrently and in no particular order (goroutine per
// entry), so never assert arrival order — across destinations OR within one.
// Tests whose semantics need a deterministic sequence pace it themselves:
// add one message, waitTerminal, add the next.
//
// Files in this suite (concern → file):
//   - characterization_harness_test.go         shared setup, doubles, helpers
//   - characterization_ordering_test.go        failure counting & thresholds
//   - characterization_idempotency_test.go     replay / idempotency
//   - characterization_acknowledgement_test.go ack/nack exactly-once
//   - characterization_validation_test.go      intake (parse / dedup / persist)
//   - characterization_decoupling_test.go      persistence decoupled from delivery
//   - characterization_postprocess_test.go     post-persist eval concurrency

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/memlogstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================== Boundary doubles ==============================

type sinkRecord struct {
	topic     string
	destID    string
	attemptID string
}

// recordingSink implements opevents.Sink. It records each emitted event, can
// inject Send errors keyed by attemptID, topic, or "attemptID/topic" (one
// attempt's one send), and can block matching sends until release() —
// simulating a slow sink. It also tracks how many sends run concurrently.
// Injected-failure events are NOT recorded (so records reflect only
// successfully delivered events).
type recordingSink struct {
	mu      sync.Mutex
	records []sinkRecord
	failOn  map[string]bool // key by attemptID, topic, or attemptID+"/"+topic

	blockOn     map[string]bool // block these sends (by attemptID or topic)...
	blockCh     chan struct{}   // ...until this closes (via release)
	releaseOnce sync.Once

	inflight    atomic.Int32
	maxInflight atomic.Int32
}

func (s *recordingSink) Init(ctx context.Context) error { return nil }
func (s *recordingSink) Close() error                   { return nil }

func (s *recordingSink) Send(ctx context.Context, event *opevents.OperatorEvent) error {
	cur := s.inflight.Add(1)
	defer s.inflight.Add(-1)
	for {
		max := s.maxInflight.Load()
		if cur <= max || s.maxInflight.CompareAndSwap(max, cur) {
			break
		}
	}

	// All three alert payloads (consecutive_failure / disabled / exhausted)
	// carry a destination with an id and an attempt with an id.
	var payload struct {
		Destination struct {
			ID string `json:"id"`
		} `json:"destination"`
		Attempt struct {
			ID string `json:"id"`
		} `json:"attempt"`
	}
	_ = json.Unmarshal(event.Data, &payload)
	destID := payload.Destination.ID
	attemptID := payload.Attempt.ID

	// Block outside the lock so a blocked send doesn't serialize the others.
	// Honors ctx like a real sink call: a canceled send returns ctx.Err()
	// (this is how the emit-timeout tests trip the deadline).
	if s.blockCh != nil && (s.blockOn[attemptID] || s.blockOn[event.Topic]) {
		select {
		case <-s.blockCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failOn[attemptID] || s.failOn[event.Topic] || s.failOn[attemptID+"/"+event.Topic] {
		return fmt.Errorf("injected send failure topic=%s attempt=%s", event.Topic, attemptID)
	}
	s.records = append(s.records, sinkRecord{topic: event.Topic, destID: destID, attemptID: attemptID})
	return nil
}

// release unblocks every blocked (and future) matching send.
func (s *recordingSink) release() {
	s.releaseOnce.Do(func() { close(s.blockCh) })
}

func (s *recordingSink) inflightSends() int32    { return s.inflight.Load() }
func (s *recordingSink) maxInflightSends() int32 { return s.maxInflight.Load() }

func (s *recordingSink) snapshot() []sinkRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sinkRecord(nil), s.records...)
}

// forDest returns the records for a single destination, in arrival order.
func (s *recordingSink) forDest(destID string) []sinkRecord {
	out := []sinkRecord{}
	for _, r := range s.snapshot() {
		if r.destID == destID {
			out = append(out, r)
		}
	}
	return out
}

// blockingEvaluator wraps the real evaluator and blocks Evaluate for matching
// attempt IDs until release() — simulating a slow eval (e.g. a Redis hiccup).
// It counts inner Evaluate calls so tests can assert an eval did NOT run yet.
type blockingEvaluator struct {
	inner       logmq.AlertEvaluator
	blockOn     map[string]bool // block these evals (by attemptID)...
	blockCh     chan struct{}   // ...until this closes (via release)
	releaseOnce sync.Once

	blocked atomic.Int32 // evals that reached the block
	entered atomic.Int32 // evals that reached the inner evaluator
}

func (e *blockingEvaluator) Evaluate(ctx context.Context, attempt alert.Attempt) (alert.Evaluation, error) {
	if e.blockOn[attempt.AttemptID] {
		e.blocked.Add(1)
		<-e.blockCh
	}
	e.entered.Add(1)
	return e.inner.Evaluate(ctx, attempt)
}

// release unblocks every blocked (and future) matching eval.
func (e *blockingEvaluator) release() {
	e.releaseOnce.Do(func() { close(e.blockCh) })
}

func (e *blockingEvaluator) blockedEvals() int32 { return e.blocked.Load() }
func (e *blockingEvaluator) enteredEvals() int32 { return e.entered.Load() }

type disableRecord struct {
	tenantID      string
	destinationID string
}

// recordingDisabler implements logmq.DestinationDisabler.
type recordingDisabler struct {
	mu       sync.Mutex
	disabled []disableRecord
}

func (d *recordingDisabler) DisableDestination(ctx context.Context, tenantID, destinationID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.disabled = append(d.disabled, disableRecord{tenantID: tenantID, destinationID: destinationID})
	return nil
}

func (d *recordingDisabler) snapshot() []disableRecord {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]disableRecord(nil), d.disabled...)
}

// countingMessage implements mqs.QueueMessage with integer counters so tests can
// assert exactly-once terminal state (ack==1, nack==0 or vice versa).
type countingMessage struct {
	ack  atomic.Int32
	nack atomic.Int32
}

func (m *countingMessage) Ack()  { m.ack.Add(1) }
func (m *countingMessage) Nack() { m.nack.Add(1) }

func (m *countingMessage) acks() int32  { return m.ack.Load() }
func (m *countingMessage) nacks() int32 { return m.nack.Load() }

// requireAcked asserts the message reached exactly one terminal state: a single
// ack and no nack.
func (m *countingMessage) requireAcked(t *testing.T) {
	t.Helper()
	assert.Equal(t, int32(1), m.acks(), "expected exactly one ack")
	assert.Equal(t, int32(0), m.nacks(), "expected no nack")
}

// requireNacked asserts the message reached exactly one terminal state: a single
// nack and no ack.
func (m *countingMessage) requireNacked(t *testing.T) {
	t.Helper()
	assert.Equal(t, int32(1), m.nacks(), "expected exactly one nack")
	assert.Equal(t, int32(0), m.acks(), "expected no ack")
}

// requireTerminalOnce asserts exactly one terminal state (ack OR nack), without
// constraining which. Use when the terminal kind is asserted separately.
func (m *countingMessage) requireTerminalOnce(t *testing.T) {
	t.Helper()
	assert.Equal(t, int32(1), m.acks()+m.nacks(), "expected exactly one terminal state")
}

// failingLogStore always errors on InsertMany. Used only for the InsertMany-error case.
type failingLogStore struct {
	err error
}

func (f *failingLogStore) InsertMany(ctx context.Context, entries []*models.LogEntry) error {
	return f.err
}

// ============================== Harness ==============================

// harnessConfig groups knobs by concern: batcher + alert configure the REAL
// pipeline components; doubles tweaks the behavior of the test doubles.
type harnessConfig struct {
	batcher batcherConfig // real BatchProcessor
	alert   alertConfig   // real alert.Evaluator
	doubles doublesConfig // test-double behavior
}

// batcherConfig drives the real BatchProcessor flush behavior.
type batcherConfig struct {
	itemCount int           // flush when this many messages buffered
	delay     time.Duration // ...or flush after this long (default 100ms)
	// emitTimeout overrides the per-send timeout (zero = production default).
	// Used with a blocked sink to trip the timeout without a 5s test.
	emitTimeout time.Duration
}

// alertConfig drives the real alert.Evaluator. Zero values fall back to defaults
// (thresholds [50,70,90,100], autoDisableCount 10, retryMaxLimit 10).
type alertConfig struct {
	thresholds       []int
	autoDisableCount int
	retryMaxLimit    int
	withDisabler     bool // attach the recordingDisabler to the pipeline
}

// doublesConfig controls test-double behavior. Zero values = passthrough: the
// sink injects no failures, blocks nothing, and the harness uses a real
// memlogstore.
type doublesConfig struct {
	sinkFailOn  map[string]bool         // make sink.Send fail for these attemptIDs/topics
	sinkBlockOn map[string]bool         // block sink.Send for these attemptIDs/topics until h.sink.release()
	evalBlockOn map[string]bool         // block Evaluate for these attemptIDs until h.eval.release()
	logStore    logmq.LogStore          // override the store (e.g. failingLogStore); nil = memlogstore
	idemp       idempotence.Idempotence // exhausted-retries suppression; nil = unsuppressed
	// failMarkProcessed makes every MarkProcessed call on the replay gate
	// error (the Processed check still works). Simulates Redis failing after
	// the attempt's events were delivered.
	failMarkProcessed bool
}

// failingMarkGate wraps a real ReplayGate and fails every MarkProcessed.
type failingMarkGate struct {
	inner logmq.ReplayGate
}

func (g *failingMarkGate) Processed(ctx context.Context, key string) (bool, error) {
	return g.inner.Processed(ctx, key)
}

func (g *failingMarkGate) MarkProcessed(ctx context.Context, key string) error {
	return fmt.Errorf("injected MarkProcessed failure key=%s", key)
}

type harness struct {
	t        *testing.T
	ctx      context.Context
	bp       *logmq.BatchProcessor
	sink     *recordingSink
	eval     *blockingEvaluator // nil unless doubles.evalBlockOn was set
	disabler *recordingDisabler
	store    driver.LogStore // nil when logStore was overridden with a non-memlogstore
}

func newHarness(t *testing.T, cfg harnessConfig) *harness {
	t.Helper()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	sink := &recordingSink{failOn: cfg.doubles.sinkFailOn, blockOn: cfg.doubles.sinkBlockOn}
	if sink.blockOn != nil {
		sink.blockCh = make(chan struct{})
	}
	disabler := &recordingDisabler{}

	// NOTE: opevents.NewEmitter returns a NOOP emitter when topics is nil/empty.
	// To accept all topics we must pass []string{"*"} (NOT nil).
	emitter := opevents.NewEmitter(sink, "test-deploy", []string{"*"})

	thresholds := cfg.alert.thresholds
	if thresholds == nil {
		thresholds = []int{50, 70, 90, 100}
	}
	autoDisableCount := cfg.alert.autoDisableCount
	if autoDisableCount == 0 {
		autoDisableCount = 10
	}
	retryMaxLimit := cfg.alert.retryMaxLimit
	if retryMaxLimit == 0 {
		retryMaxLimit = 10
	}
	var evaluator logmq.AlertEvaluator = alert.NewEvaluator(alert.NewRedisAlertStore(redisClient, ""), retryMaxLimit,
		alert.WithAutoDisableFailureCount(autoDisableCount),
		alert.WithAlertThresholds(thresholds),
	)
	var evalDouble *blockingEvaluator
	if cfg.doubles.evalBlockOn != nil {
		evalDouble = &blockingEvaluator{
			inner:   evaluator,
			blockOn: cfg.doubles.evalBlockOn,
			blockCh: make(chan struct{}),
		}
		evaluator = evalDouble
	}

	var logStore logmq.LogStore
	var store driver.LogStore
	if cfg.doubles.logStore != nil {
		logStore = cfg.doubles.logStore
	} else {
		store = memlogstore.NewLogStore()
		logStore = store
	}

	delay := cfg.batcher.delay
	if delay == 0 {
		delay = 100 * time.Millisecond
	}
	// The suite wires the REAL processed gate (per-attempt replay dedup) over
	// the same miniredis. No exhausted suppression window by default (emit on
	// every exhaustion); delivery tests opt in via doubles.idemp. The disabler
	// attaches to the pipeline when alert.withDisabler is set.
	var gate logmq.ReplayGate = idempotence.New(redisClient)
	if cfg.doubles.failMarkProcessed {
		gate = &failingMarkGate{inner: gate}
	}
	pipeline := logmq.AlertPipeline{
		Evaluator:      evaluator,
		Emitter:        emitter,
		ProcessedIdemp: gate,
		ExhaustedIdemp: cfg.doubles.idemp,
	}
	if cfg.alert.withDisabler {
		pipeline.Disabler = disabler
	}
	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, pipeline, logmq.BatchProcessorConfig{
		ItemCountThreshold: cfg.batcher.itemCount,
		DelayThreshold:     delay,
		EmitTimeout:        cfg.batcher.emitTimeout,
	})
	require.NoError(t, err)
	t.Cleanup(bp.Shutdown)
	// LIFO: releases run BEFORE bp.Shutdown, so a test that never released its
	// blocked sends/evals can't deadlock the drain.
	if sink.blockCh != nil {
		t.Cleanup(sink.release)
	}
	if evalDouble != nil {
		t.Cleanup(evalDouble.release)
	}

	return &harness{t: t, ctx: ctx, bp: bp, sink: sink, eval: evalDouble, disabler: disabler, store: store}
}

// makeEntry builds a LogEntry with event+attempt+destination all populated.
// AttemptNumber defaults to 1 so memlogstore persists the event (it only inserts
// the event when AttemptNumber <= 1), which lets ListAttempt link the record.
func makeEntry(destID, tenantID, attemptID, status string) models.LogEntry {
	return makeEntryFull(destID, tenantID, attemptID, status, 1, true)
}

func makeEntryFull(destID, tenantID, attemptID, status string, attemptNumber int, eligibleForRetry bool) models.LogEntry {
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithEligibleForRetry(eligibleForRetry),
		testutil.EventFactory.WithMatchedDestinationIDs([]string{destID}),
	)
	attempt := testutil.AttemptFactory.Any(
		testutil.AttemptFactory.WithID(attemptID),
		testutil.AttemptFactory.WithTenantID(tenantID),
		testutil.AttemptFactory.WithEventID(event.ID),
		testutil.AttemptFactory.WithDestinationID(destID),
		testutil.AttemptFactory.WithStatus(status),
		testutil.AttemptFactory.WithAttemptNumber(attemptNumber),
	)
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID(destID),
		testutil.DestinationFactory.WithTenantID(tenantID),
	)
	return models.LogEntry{Event: &event, Attempt: &attempt, Destination: &dest}
}

func newCountingMessage(entry models.LogEntry) (*countingMessage, *mqs.Message) {
	body, _ := json.Marshal(entry)
	cm := &countingMessage{}
	return cm, &mqs.Message{QueueMessage: cm, Body: body, LoggableID: "test-msg"}
}

func newRawMessage(body []byte) (*countingMessage, *mqs.Message) {
	cm := &countingMessage{}
	return cm, &mqs.Message{QueueMessage: cm, Body: body, LoggableID: "test-msg"}
}

// add pushes a message into the batcher.
func (h *harness) add(msg *mqs.Message) {
	require.NoError(h.t, h.bp.Add(h.ctx, msg))
}

// waitTerminal blocks until every message has reached exactly one terminal state.
func (h *harness) waitTerminal(msgs []*countingMessage) {
	h.t.Helper()
	require.Eventually(h.t, func() bool {
		for _, m := range msgs {
			if m.acks()+m.nacks() == 0 {
				return false
			}
		}
		return true
	}, 5*time.Second, 5*time.Millisecond, "all messages should reach a terminal state")
}

// listAttempt returns the persisted attempt records for a destination.
func (h *harness) listAttempt(destID string) []*driver.AttemptRecord {
	resp, err := h.store.ListAttempt(h.ctx, driver.ListAttemptRequest{DestinationIDs: []string{destID}})
	require.NoError(h.t, err)
	return resp.Data
}

// topics extracts the topic sequence from a record slice.
func topics(recs []sinkRecord) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.topic
	}
	return out
}

// attemptIDs extracts the attemptID sequence from a record slice.
func attemptIDs(recs []sinkRecord) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.attemptID
	}
	return out
}

// topicsForAttempt extracts one attempt's topics. WHICH topics an attempt
// emitted is guaranteed; arrival order is not — an attempt's sends run
// concurrently, so assert with ElementsMatch.
func topicsForAttempt(recs []sinkRecord, attemptID string) []string {
	out := []string{}
	for _, r := range recs {
		if r.attemptID == attemptID {
			out = append(out, r.topic)
		}
	}
	return out
}

// forTopic filters a record slice down to one topic — e.g. to assert which
// attempts carried the cf alerts without the per-attempt attempt.* noise.
func forTopic(recs []sinkRecord, topic string) []sinkRecord {
	out := []sinkRecord{}
	for _, r := range recs {
		if r.topic == topic {
			out = append(out, r)
		}
	}
	return out
}

// repeatTopic builds an expected-topics slice: n copies of topic, then rest.
// Every attempt emits its attempt.success/attempt.failed event, so expected
// multisets are mostly "one per attempt, plus the alerts".
func repeatTopic(topic string, n int, rest ...string) []string {
	out := make([]string, 0, n+len(rest))
	for range n {
		out = append(out, topic)
	}
	return append(out, rest...)
}

const (
	topicCF       = opevents.TopicAlertConsecutiveFailure
	topicDisabled = opevents.TopicAlertDestinationDisabled
	topicExhaust  = opevents.TopicAlertExhaustedRetries
	topicSuccess  = opevents.TopicAttemptSuccess
	topicFailed   = opevents.TopicAttemptFailed
)
