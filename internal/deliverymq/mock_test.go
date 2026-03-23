package deliverymq_test

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	mqs "github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/stretchr/testify/mock"
)

type mockPublisher struct {
	responses []error
	current   int
	mu        sync.Mutex
}

func newMockPublisher(responses []error) *mockPublisher {
	return &mockPublisher{responses: responses}
}

func (m *mockPublisher) PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) (*models.Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current >= len(m.responses) {
		m.current++
		return &models.Attempt{
			ID:            idgen.Attempt(),
			EventID:       event.ID,
			DestinationID: destination.ID,
			Status:        models.AttemptStatusSuccess,
			Code:          "OK",
			ResponseData:  map[string]interface{}{},
			Time:          time.Now(),
		}, nil
	}

	resp := m.responses[m.current]
	m.current++
	if resp == nil {
		return &models.Attempt{
			ID:            idgen.Attempt(),
			EventID:       event.ID,
			DestinationID: destination.ID,
			Status:        models.AttemptStatusSuccess,
			Code:          "OK",
			ResponseData:  map[string]interface{}{},
			Time:          time.Now(),
		}, nil
	}
	return &models.Attempt{
		ID:            idgen.Attempt(),
		EventID:       event.ID,
		DestinationID: destination.ID,
		Status:        models.AttemptStatusFailed,
		Code:          "ERR",
		ResponseData:  map[string]interface{}{},
		Time:          time.Now(),
	}, resp
}

func (m *mockPublisher) Current() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

type mockDestinationGetter struct {
	dest    *models.Destination
	err     error
	current int
}

func (m *mockDestinationGetter) RetrieveDestination(ctx context.Context, tenantID, destID string) (*models.Destination, error) {
	m.current++
	return m.dest, m.err
}

// mockMultiDestinationGetter supports multiple destinations keyed by destination ID
type mockMultiDestinationGetter struct {
	destinations map[string]*models.Destination
	err          error
}

func newMockMultiDestinationGetter() *mockMultiDestinationGetter {
	return &mockMultiDestinationGetter{
		destinations: make(map[string]*models.Destination),
	}
}

func (m *mockMultiDestinationGetter) registerDestination(dest *models.Destination) {
	m.destinations[dest.ID] = dest
}

func (m *mockMultiDestinationGetter) RetrieveDestination(ctx context.Context, tenantID, destID string) (*models.Destination, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.destinations[destID], nil
}

type mockEventGetter struct {
	records []*logstore.AttemptRecord // tracks logged attempts with event data
	err     error
}

func newMockEventGetter() *mockEventGetter {
	return &mockEventGetter{
		records: make([]*logstore.AttemptRecord, 0),
	}
}

func (m *mockEventGetter) addRecord(attempt *models.Attempt, event *models.Event) {
	m.records = append(m.records, &logstore.AttemptRecord{Attempt: attempt, Event: event})
}

func (m *mockEventGetter) ListAttempt(ctx context.Context, req logstore.ListAttemptRequest) (logstore.ListAttemptResponse, error) {
	if m.err != nil {
		return logstore.ListAttemptResponse{}, m.err
	}
	// Filter records matching the request criteria
	var matched []*logstore.AttemptRecord
	for _, r := range m.records {
		if len(req.EventIDs) > 0 && !contains(req.EventIDs, r.Attempt.EventID) {
			continue
		}
		if len(req.DestinationIDs) > 0 && !contains(req.DestinationIDs, r.Attempt.DestinationID) {
			continue
		}
		matched = append(matched, r)
	}
	// Sort desc by AttemptNumber (highest first)
	for i := 0; i < len(matched); i++ {
		for j := i + 1; j < len(matched); j++ {
			if matched[j].Attempt.AttemptNumber > matched[i].Attempt.AttemptNumber {
				matched[i], matched[j] = matched[j], matched[i]
			}
		}
	}
	if req.Limit > 0 && len(matched) > req.Limit {
		matched = matched[:req.Limit]
	}
	return logstore.ListAttemptResponse{Data: matched}, nil
}

// mockDelayedEventGetter simulates the race condition where logstore data is not yet
// persisted when retry scheduler first queries it.
// Returns empty for the first N calls, then returns the attempt record.
type mockDelayedEventGetter struct {
	record          *logstore.AttemptRecord
	callCount       int
	returnAfterCall int // Return record after this many calls
	mu              sync.Mutex
}

func newMockDelayedEventGetter(event *models.Event, returnAfterCall int) *mockDelayedEventGetter {
	return &mockDelayedEventGetter{
		record: &logstore.AttemptRecord{
			Attempt: &models.Attempt{
				EventID:       event.ID,
				DestinationID: event.DestinationID,
				AttemptNumber: 0,
			},
			Event: event,
		},
		returnAfterCall: returnAfterCall,
	}
}

func (m *mockDelayedEventGetter) ListAttempt(ctx context.Context, req logstore.ListAttemptRequest) (logstore.ListAttemptResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.callCount <= m.returnAfterCall {
		// Simulate data not yet persisted
		return logstore.ListAttemptResponse{}, nil
	}
	return logstore.ListAttemptResponse{Data: []*logstore.AttemptRecord{m.record}}, nil
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

type mockLogPublisher struct {
	err          error
	entries      []models.LogEntry
	eventGetter  *mockEventGetter // if set, feed logged attempts to this getter
}

func newMockLogPublisher(err error) *mockLogPublisher {
	return &mockLogPublisher{
		err:     err,
		entries: make([]models.LogEntry, 0),
	}
}

func (m *mockLogPublisher) Publish(ctx context.Context, entry models.LogEntry) error {
	m.entries = append(m.entries, entry)
	// Feed attempt+event to mockEventGetter so ListAttempt returns correct data
	if m.eventGetter != nil && entry.Attempt != nil {
		m.eventGetter.addRecord(entry.Attempt, entry.Event)
	}
	return m.err
}

// scheduledEntry represents a retry entry in the stateful mock scheduler.
// Mirrors the real scheduler's upsert semantics: Schedule with the same ID
// overwrites the previous entry, Cancel removes it.
type scheduledEntry struct {
	task  string
	delay time.Duration
}

type mockRetryScheduler struct {
	// Call-recording fields (used by existing tests)
	schedules    []string
	taskIDs      []string
	canceled     []string
	scheduleResp []error
	cancelResp   []error
	scheduleIdx  int
	cancelIdx    int

	// Stateful map: tracks the current set of scheduled retries.
	// Schedule upserts by ID, Cancel deletes by ID — matching real
	// scheduler behavior (RSMQ ZAdd+HSet overwrites, DeleteMessage removes).
	entries map[string]scheduledEntry
}

func newMockRetryScheduler() *mockRetryScheduler {
	return &mockRetryScheduler{
		schedules:    make([]string, 0),
		taskIDs:      make([]string, 0),
		canceled:     make([]string, 0),
		scheduleResp: make([]error, 0),
		cancelResp:   make([]error, 0),
		entries:      make(map[string]scheduledEntry),
	}
}

func (m *mockRetryScheduler) Schedule(ctx context.Context, task string, delay time.Duration, opts ...scheduler.ScheduleOption) error {
	m.schedules = append(m.schedules, task)

	// Capture the task ID by applying the option
	options := &scheduler.ScheduleOptions{}
	for _, opt := range opts {
		opt(options)
	}
	if options.ID != "" {
		m.taskIDs = append(m.taskIDs, options.ID)
		// Upsert into stateful map
		m.entries[options.ID] = scheduledEntry{task: task, delay: delay}
	}

	if m.scheduleIdx < len(m.scheduleResp) {
		err := m.scheduleResp[m.scheduleIdx]
		m.scheduleIdx++
		return err
	}
	return nil
}

func (m *mockRetryScheduler) Cancel(ctx context.Context, taskID string) error {
	m.canceled = append(m.canceled, taskID)
	delete(m.entries, taskID)

	if m.cancelIdx < len(m.cancelResp) {
		err := m.cancelResp[m.cancelIdx]
		m.cancelIdx++
		return err
	}
	return nil
}

type mockMessage struct {
	id     string
	acked  bool
	nacked bool
}

func newDeliveryMockMessage(task models.DeliveryTask) (*mockMessage, *mqs.Message) {
	mock := &mockMessage{id: task.IdempotencyKey()}
	body, err := json.Marshal(task)
	if err != nil {
		panic(err)
	}
	return mock, &mqs.Message{
		QueueMessage: mock,
		Body:         body,
	}
}

func (m *mockMessage) ID() string {
	return m.id
}

func (m *mockMessage) Ack() {
	m.acked = true
}

func (m *mockMessage) Nack() {
	m.nacked = true
}

func (m *mockMessage) Data() []byte {
	return nil
}

func (m *mockMessage) SetData([]byte) {}

type mockAlertMonitor struct {
	mock.Mock
}

func (m *mockAlertMonitor) HandleAttempt(ctx context.Context, attempt alert.DeliveryAttempt) error {
	args := m.Called(ctx, attempt)
	return args.Error(0)
}

func newMockAlertMonitor() *mockAlertMonitor {
	monitor := &mockAlertMonitor{}
	// Set up default expectation to handle any attempt
	monitor.On("HandleAttempt", mock.Anything, mock.MatchedBy(func(attempt alert.DeliveryAttempt) bool {
		return true // Accept any attempt
	})).Return(nil)
	return monitor
}
