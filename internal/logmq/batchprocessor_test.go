package logmq_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"errors"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLogStore struct {
	mu      sync.Mutex
	entries []*models.LogEntry
	err     error
}

func (m *mockLogStore) InsertMany(ctx context.Context, entries []*models.LogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.entries = append(m.entries, entries...)
	return nil
}

func (m *mockLogStore) getInserted() (events []*models.Event, attempts []*models.Attempt) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.entries {
		events = append(events, entry.Event)
		attempts = append(attempts, entry.Attempt)
	}
	return events, attempts
}

// mockQueueMessage implements mqs.QueueMessage for testing. Terminal state is
// atomic: acks/nacks land on per-entry goroutines.
type mockQueueMessage struct {
	acked  atomic.Bool
	nacked atomic.Bool
}

func (m *mockQueueMessage) Ack()  { m.acked.Store(true) }
func (m *mockQueueMessage) Nack() { m.nacked.Store(true) }

func newMockMessage(entry models.LogEntry) (*mockQueueMessage, *mqs.Message) {
	body, _ := json.Marshal(entry)
	mock := &mockQueueMessage{}
	msg := &mqs.Message{
		QueueMessage: mock,
		Body:         body,
		LoggableID:   "test-msg",
	}
	return mock, msg
}

func newMockMessageFromBytes(body []byte) (*mockQueueMessage, *mqs.Message) {
	mock := &mockQueueMessage{}
	msg := &mqs.Message{
		QueueMessage: mock,
		Body:         body,
		LoggableID:   "test-msg",
	}
	return mock, msg
}

func TestBatchProcessor_ValidEntry(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, logmq.AlertPipeline{}, logmq.BatchProcessorConfig{
		ItemCountThreshold: 1,
		DelayThreshold:     1 * time.Second,
	})
	require.NoError(t, err)
	defer bp.Shutdown()

	event := testutil.EventFactory.Any()
	attempt := testutil.AttemptFactory.Any()
	entry := models.LogEntry{
		Event:   &event,
		Attempt: &attempt,
	}

	mock, msg := newMockMessage(entry)
	err = bp.Add(ctx, msg)
	require.NoError(t, err)

	// Wait for batch to process
	time.Sleep(200 * time.Millisecond)

	assert.True(t, mock.acked.Load(), "valid message should be acked")
	assert.False(t, mock.nacked.Load(), "valid message should not be nacked")

	events, attempts := logStore.getInserted()
	assert.Len(t, events, 1)
	assert.Len(t, attempts, 1)
}

func TestBatchProcessor_InvalidEntry_MissingEvent(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, logmq.AlertPipeline{}, logmq.BatchProcessorConfig{
		ItemCountThreshold: 1,
		DelayThreshold:     1 * time.Second,
	})
	require.NoError(t, err)
	defer bp.Shutdown()

	attempt := testutil.AttemptFactory.Any()
	entry := models.LogEntry{
		Event:   nil, // Missing event
		Attempt: &attempt,
	}

	mock, msg := newMockMessage(entry)
	err = bp.Add(ctx, msg)
	require.NoError(t, err)

	// Wait for batch to process
	time.Sleep(200 * time.Millisecond)

	assert.False(t, mock.acked.Load(), "invalid message should not be acked")
	assert.True(t, mock.nacked.Load(), "invalid message should be nacked")

	events, attempts := logStore.getInserted()
	assert.Empty(t, events, "no events should be inserted for invalid entry")
	assert.Empty(t, attempts, "no attempts should be inserted for invalid entry")
}

func TestBatchProcessor_InvalidEntry_MissingAttempt(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, logmq.AlertPipeline{}, logmq.BatchProcessorConfig{
		ItemCountThreshold: 1,
		DelayThreshold:     1 * time.Second,
	})
	require.NoError(t, err)
	defer bp.Shutdown()

	event := testutil.EventFactory.Any()
	entry := models.LogEntry{
		Event:   &event,
		Attempt: nil, // Missing attempt
	}

	mock, msg := newMockMessage(entry)
	err = bp.Add(ctx, msg)
	require.NoError(t, err)

	// Wait for batch to process
	time.Sleep(200 * time.Millisecond)

	assert.False(t, mock.acked.Load(), "invalid message should not be acked")
	assert.True(t, mock.nacked.Load(), "invalid message should be nacked")

	events, attempts := logStore.getInserted()
	assert.Empty(t, events, "no events should be inserted for invalid entry")
	assert.Empty(t, attempts, "no attempts should be inserted for invalid entry")
}

func TestBatchProcessor_InvalidEntry_DoesNotBlockBatch(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, logmq.AlertPipeline{}, logmq.BatchProcessorConfig{
		ItemCountThreshold: 3, // Wait for 3 messages before processing
		DelayThreshold:     1 * time.Second,
	})
	require.NoError(t, err)
	defer bp.Shutdown()

	// Create valid entry 1
	event1 := testutil.EventFactory.Any()
	attempt1 := testutil.AttemptFactory.Any()
	validEntry1 := models.LogEntry{Event: &event1, Attempt: &attempt1}
	mock1, msg1 := newMockMessage(validEntry1)

	// Create invalid entry (missing event)
	attempt2 := testutil.AttemptFactory.Any()
	invalidEntry := models.LogEntry{Event: nil, Attempt: &attempt2}
	mock2, msg2 := newMockMessage(invalidEntry)

	// Create valid entry 2
	event3 := testutil.EventFactory.Any()
	attempt3 := testutil.AttemptFactory.Any()
	validEntry2 := models.LogEntry{Event: &event3, Attempt: &attempt3}
	mock3, msg3 := newMockMessage(validEntry2)

	// Add all messages
	require.NoError(t, bp.Add(ctx, msg1))
	require.NoError(t, bp.Add(ctx, msg2))
	require.NoError(t, bp.Add(ctx, msg3))

	// Wait for batch to process
	time.Sleep(200 * time.Millisecond)

	// Valid messages should be acked
	assert.True(t, mock1.acked.Load(), "valid message 1 should be acked")
	assert.False(t, mock1.nacked.Load(), "valid message 1 should not be nacked")

	// Invalid message should be nacked
	assert.False(t, mock2.acked.Load(), "invalid message should not be acked")
	assert.True(t, mock2.nacked.Load(), "invalid message should be nacked")

	// Valid message 2 should be acked (not blocked by invalid message)
	assert.True(t, mock3.acked.Load(), "valid message 2 should be acked")
	assert.False(t, mock3.nacked.Load(), "valid message 2 should not be nacked")

	// Only valid entries should be inserted
	events, attempts := logStore.getInserted()
	assert.Len(t, events, 2, "only 2 valid events should be inserted")
	assert.Len(t, attempts, 2, "only 2 valid attempts should be inserted")
}

func TestBatchProcessor_DuplicateMessages(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}
	alertMon := &mockAlertEvaluator{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, testAlertPipeline(t, alertMon), logmq.BatchProcessorConfig{
		ItemCountThreshold: 3, // Wait for 3 messages before processing
		DelayThreshold:     1 * time.Second,
	})
	require.NoError(t, err)
	defer bp.Shutdown()

	// Two byte-identical copies of the same entry (redelivery / re-publish)
	event := testutil.EventFactory.Any()
	attempt := testutil.AttemptFactory.Any()
	dest := testutil.DestinationFactory.Any()
	entry := models.LogEntry{Event: &event, Attempt: &attempt, Destination: &dest}
	mock1, msg1 := newMockMessage(entry)
	mock2, msg2 := newMockMessage(entry)

	// One distinct entry
	otherEvent := testutil.EventFactory.Any()
	otherAttempt := testutil.AttemptFactory.Any()
	otherDest := testutil.DestinationFactory.Any()
	otherEntry := models.LogEntry{Event: &otherEvent, Attempt: &otherAttempt, Destination: &otherDest}
	mock3, msg3 := newMockMessage(otherEntry)

	require.NoError(t, bp.Add(ctx, msg1))
	require.NoError(t, bp.Add(ctx, msg2))
	require.NoError(t, bp.Add(ctx, msg3))

	// Wait for batch to process
	time.Sleep(200 * time.Millisecond)

	// All copies acked, none nacked
	assert.True(t, mock1.acked.Load(), "kept copy should be acked")
	assert.False(t, mock1.nacked.Load())
	assert.True(t, mock2.acked.Load(), "duplicate copy should be acked")
	assert.False(t, mock2.nacked.Load())
	assert.True(t, mock3.acked.Load(), "distinct message should be acked")
	assert.False(t, mock3.nacked.Load())

	// Duplicate inserted once
	_, attempts := logStore.getInserted()
	require.Len(t, attempts, 2, "duplicate should be inserted once")
	attemptIDs := []string{attempts[0].ID, attempts[1].ID}
	assert.ElementsMatch(t, []string{attempt.ID, otherAttempt.ID}, attemptIDs)

	// Alert eval once per unique attempt
	calls := alertMon.getCalls()
	require.Len(t, calls, 2, "alert eval should run once per unique attempt")
}

func TestBatchProcessor_MalformedJSON(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, logmq.AlertPipeline{}, logmq.BatchProcessorConfig{
		ItemCountThreshold: 1,
		DelayThreshold:     1 * time.Second,
	})
	require.NoError(t, err)
	defer bp.Shutdown()

	mock, msg := newMockMessageFromBytes([]byte("not valid json"))
	err = bp.Add(ctx, msg)
	require.NoError(t, err)

	// Wait for batch to process
	time.Sleep(200 * time.Millisecond)

	assert.False(t, mock.acked.Load(), "malformed message should not be acked")
	assert.True(t, mock.nacked.Load(), "malformed message should be nacked")

	events, attempts := logStore.getInserted()
	assert.Empty(t, events)
	assert.Empty(t, attempts)
}

// mockAlertEvaluator records Evaluate calls and returns a configurable error.
type mockAlertEvaluator struct {
	mu        sync.Mutex
	calls     []alert.Attempt
	returnErr error
}

func (m *mockAlertEvaluator) Evaluate(ctx context.Context, attempt alert.Attempt) (alert.Evaluation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, attempt)
	if m.returnErr != nil {
		return alert.Evaluation{}, m.returnErr
	}
	return alert.Evaluation{}, nil
}

func (m *mockAlertEvaluator) getCalls() []alert.Attempt {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]alert.Attempt(nil), m.calls...)
}

// testAlertPipeline wraps an evaluator with the required delivery deps: a
// noop-sink emitter and a real (miniredis-backed) processed gate.
func testAlertPipeline(t *testing.T, evaluator logmq.AlertEvaluator) logmq.AlertPipeline {
	t.Helper()
	return logmq.AlertPipeline{
		Evaluator:      evaluator,
		Emitter:        opevents.NewEmitter(&opevents.NoopSink{}, "test-deploy", []string{"*"}, testutil.CreateTestLogger(t)),
		ProcessedIdemp: idempotence.New(testutil.CreateTestRedisClient(t)),
	}
}

func TestBatchProcessor_AlertEvaluator_WithDestination(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}
	alertMon := &mockAlertEvaluator{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, testAlertPipeline(t, alertMon), logmq.BatchProcessorConfig{
		ItemCountThreshold: 1,
		DelayThreshold:     1 * time.Second,
	})
	require.NoError(t, err)
	defer bp.Shutdown()

	event := testutil.EventFactory.Any()
	attempt := testutil.AttemptFactory.Any()
	dest := testutil.DestinationFactory.Any()
	entry := models.LogEntry{
		Event:       &event,
		Attempt:     &attempt,
		Destination: &dest,
	}

	mock, msg := newMockMessage(entry)
	require.NoError(t, bp.Add(ctx, msg))

	time.Sleep(200 * time.Millisecond)

	assert.True(t, mock.acked.Load(), "should be acked when alert evaluation succeeds")
	assert.False(t, mock.nacked.Load())

	calls := alertMon.getCalls()
	require.Len(t, calls, 1, "alert evaluator should have been called once")
	assert.Equal(t, dest.ID, calls[0].DestinationID)
}

func TestBatchProcessor_AlertEvaluator_NilDestination(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}
	alertMon := &mockAlertEvaluator{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, testAlertPipeline(t, alertMon), logmq.BatchProcessorConfig{
		ItemCountThreshold: 1,
		DelayThreshold:     1 * time.Second,
	})
	require.NoError(t, err)
	defer bp.Shutdown()

	event := testutil.EventFactory.Any()
	attempt := testutil.AttemptFactory.Any()
	entry := models.LogEntry{
		Event:       &event,
		Attempt:     &attempt,
		Destination: nil, // migration case
	}

	mock, msg := newMockMessage(entry)
	require.NoError(t, bp.Add(ctx, msg))

	time.Sleep(200 * time.Millisecond)

	assert.True(t, mock.acked.Load(), "should be acked even without destination (migration grace)")
	assert.False(t, mock.nacked.Load())
	assert.Empty(t, alertMon.getCalls(), "alert evaluator should not be called without destination")
}

func TestBatchProcessor_AlertEvaluator_Error(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}
	alertMon := &mockAlertEvaluator{returnErr: errors.New("alert failed")}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, testAlertPipeline(t, alertMon), logmq.BatchProcessorConfig{
		ItemCountThreshold: 1,
		DelayThreshold:     1 * time.Second,
	})
	require.NoError(t, err)
	defer bp.Shutdown()

	event := testutil.EventFactory.Any()
	attempt := testutil.AttemptFactory.Any()
	dest := testutil.DestinationFactory.Any()
	entry := models.LogEntry{
		Event:       &event,
		Attempt:     &attempt,
		Destination: &dest,
	}

	mock, msg := newMockMessage(entry)
	require.NoError(t, bp.Add(ctx, msg))

	time.Sleep(200 * time.Millisecond)

	assert.False(t, mock.acked.Load(), "should not be acked when alert evaluation fails")
	assert.True(t, mock.nacked.Load(), "should be nacked when alert evaluation fails")

	// Entry was still persisted to logstore
	events, _ := logStore.getInserted()
	assert.Len(t, events, 1, "log entry should still be persisted despite alert failure")
}
