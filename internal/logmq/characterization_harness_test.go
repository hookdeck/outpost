package logmq_test

// Characterization suite for the logmq post-persist pipeline.
//
// These tests pin the CURRENT behavior of BatchProcessor.processBatch wired to
// the REAL alert monitor, REAL in-memory log store, REAL opevents emitter and a
// miniredis-backed alert store. The only doubles are at the external boundary:
//   - recordingSink     (opevents.Sink): records emitted operator events, can
//     inject Send failures.
//   - recordingDisabler (alert.DestinationDisabler): records disable calls.
//   - countingMessage   (mqs.QueueMessage): counts ack/nack for exactly-once.
//
// Observable oracles (assert ONLY on these):
//   - sink.records   (ordered list of {topic, destID, attemptID})
//   - per-message ack/nack counters (exactly-once terminal state)
//   - logStore.ListAttempt / ListEvent
//
// Per-destination order only — never assert global cross-destination order.
//
// Files in this suite (concern → file):
//   - characterization_harness_test.go         shared setup, doubles, helpers
//   - characterization_ordering_test.go        ordering & counting
//   - characterization_idempotency_test.go     replay / idempotency
//   - characterization_acknowledgement_test.go ack/nack exactly-once
//   - characterization_validation_test.go      intake (parse / dedup / persist)

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
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

// recordingSink implements opevents.Sink. It records each emitted event and can
// inject Send errors keyed by attemptID or topic. Injected-failure events are
// NOT recorded (so records reflect only successfully delivered events).
type recordingSink struct {
	mu      sync.Mutex
	records []sinkRecord
	failOn  map[string]bool // key by attemptID or topic
}

func (s *recordingSink) Init(ctx context.Context) error { return nil }
func (s *recordingSink) Close() error                   { return nil }

func (s *recordingSink) Send(ctx context.Context, event *opevents.OperatorEvent) error {
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

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failOn[attemptID] || s.failOn[event.Topic] {
		return fmt.Errorf("injected send failure topic=%s attempt=%s", event.Topic, attemptID)
	}
	s.records = append(s.records, sinkRecord{topic: event.Topic, destID: destID, attemptID: attemptID})
	return nil
}

func (s *recordingSink) snapshot() []sinkRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sinkRecord(nil), s.records...)
}

// forDest returns the records for a single destination, in emission order.
func (s *recordingSink) forDest(destID string) []sinkRecord {
	out := []sinkRecord{}
	for _, r := range s.snapshot() {
		if r.destID == destID {
			out = append(out, r)
		}
	}
	return out
}

type disableRecord struct {
	tenantID      string
	destinationID string
}

// recordingDisabler implements alert.DestinationDisabler.
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
	alert   alertConfig   // real AlertMonitor
	doubles doublesConfig // test-double behavior
}

// batcherConfig drives the real BatchProcessor flush behavior.
type batcherConfig struct {
	itemCount int           // flush when this many messages buffered
	delay     time.Duration // ...or flush after this long (default 100ms)
}

// alertConfig drives the real AlertMonitor. Zero values fall back to defaults
// (thresholds [50,70,90,100], autoDisableCount 10, retryMaxLimit 10).
type alertConfig struct {
	thresholds       []int
	autoDisableCount int
	retryMaxLimit    int
	withDisabler     bool // attach the recordingDisabler to the monitor
}

// doublesConfig controls test-double behavior. Zero values = passthrough: the
// sink injects no failures and the harness uses a real memlogstore.
type doublesConfig struct {
	sinkFailOn map[string]bool // make sink.Send fail for these attemptIDs/topics
	logStore   logmq.LogStore  // override the store (e.g. failingLogStore); nil = memlogstore
}

type harness struct {
	t        *testing.T
	ctx      context.Context
	bp       *logmq.BatchProcessor
	sink     *recordingSink
	disabler *recordingDisabler
	store    driver.LogStore // nil when logStore was overridden with a non-memlogstore
}

func newHarness(t *testing.T, cfg harnessConfig) *harness {
	t.Helper()
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	sink := &recordingSink{failOn: cfg.doubles.sinkFailOn}
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
	opts := []alert.AlertOption{
		alert.WithAutoDisableFailureCount(autoDisableCount),
		alert.WithAlertThresholds(thresholds),
	}
	if cfg.alert.withDisabler {
		opts = append(opts, alert.WithDisabler(disabler))
	}
	monitor := alert.NewAlertMonitor(logger, redisClient, emitter, retryMaxLimit, opts...)

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
	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, monitor, logmq.BatchProcessorConfig{
		ItemCountThreshold: cfg.batcher.itemCount,
		DelayThreshold:     delay,
	})
	require.NoError(t, err)
	t.Cleanup(bp.Shutdown)

	return &harness{t: t, ctx: ctx, bp: bp, sink: sink, disabler: disabler, store: store}
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

const (
	topicCF       = opevents.TopicAlertConsecutiveFailure
	topicDisabled = opevents.TopicAlertDestinationDisabled
	topicExhaust  = opevents.TopicAlertExhaustedRetries
)
