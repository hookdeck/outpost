package logmq_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
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

// mockQueueMessage implements mqs.QueueMessage for testing.
type mockQueueMessage struct {
	acked  bool
	nacked bool
}

func (m *mockQueueMessage) Ack()  { m.acked = true }
func (m *mockQueueMessage) Nack() { m.nacked = true }

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

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, logmq.BatchProcessorConfig{
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

	assert.True(t, mock.acked, "valid message should be acked")
	assert.False(t, mock.nacked, "valid message should not be nacked")

	events, deliveries := logStore.getInserted()
	assert.Len(t, events, 1)
	assert.Len(t, deliveries, 1)
}

func TestBatchProcessor_InvalidEntry_MissingEvent(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, logmq.BatchProcessorConfig{
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

	assert.False(t, mock.acked, "invalid message should not be acked")
	assert.True(t, mock.nacked, "invalid message should be nacked")

	events, deliveries := logStore.getInserted()
	assert.Empty(t, events, "no events should be inserted for invalid entry")
	assert.Empty(t, deliveries, "no deliveries should be inserted for invalid entry")
}

func TestBatchProcessor_InvalidEntry_MissingDelivery(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, logmq.BatchProcessorConfig{
		ItemCountThreshold: 1,
		DelayThreshold:     1 * time.Second,
	})
	require.NoError(t, err)
	defer bp.Shutdown()

	event := testutil.EventFactory.Any()
	entry := models.LogEntry{
		Event:   &event,
		Attempt: nil, // Missing delivery
	}

	mock, msg := newMockMessage(entry)
	err = bp.Add(ctx, msg)
	require.NoError(t, err)

	// Wait for batch to process
	time.Sleep(200 * time.Millisecond)

	assert.False(t, mock.acked, "invalid message should not be acked")
	assert.True(t, mock.nacked, "invalid message should be nacked")

	events, deliveries := logStore.getInserted()
	assert.Empty(t, events, "no events should be inserted for invalid entry")
	assert.Empty(t, deliveries, "no deliveries should be inserted for invalid entry")
}

func TestBatchProcessor_InvalidEntry_DoesNotBlockBatch(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, logmq.BatchProcessorConfig{
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
	assert.True(t, mock1.acked, "valid message 1 should be acked")
	assert.False(t, mock1.nacked, "valid message 1 should not be nacked")

	// Invalid message should be nacked
	assert.False(t, mock2.acked, "invalid message should not be acked")
	assert.True(t, mock2.nacked, "invalid message should be nacked")

	// Valid message 2 should be acked (not blocked by invalid message)
	assert.True(t, mock3.acked, "valid message 2 should be acked")
	assert.False(t, mock3.nacked, "valid message 2 should not be nacked")

	// Only valid entries should be inserted
	events, deliveries := logStore.getInserted()
	assert.Len(t, events, 2, "only 2 valid events should be inserted")
	assert.Len(t, deliveries, 2, "only 2 valid deliveries should be inserted")
}

func TestBatchProcessor_MalformedJSON(t *testing.T) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	logStore := &mockLogStore{}

	bp, err := logmq.NewBatchProcessor(ctx, logger, logStore, logmq.BatchProcessorConfig{
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

	assert.False(t, mock.acked, "malformed message should not be acked")
	assert.True(t, mock.nacked, "malformed message should be nacked")

	events, deliveries := logStore.getInserted()
	assert.Empty(t, events)
	assert.Empty(t, deliveries)
}
