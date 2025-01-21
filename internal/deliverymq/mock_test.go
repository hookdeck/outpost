package deliverymq_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/models"
	mqs "github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/stretchr/testify/mock"
)

// scheduleOptions mirrors the private type in scheduler package
type scheduleOptions struct {
	id string
}

type mockPublisher struct {
	responses []error
	current   int
	mu        sync.Mutex
}

func newMockPublisher(responses []error) *mockPublisher {
	return &mockPublisher{responses: responses}
}

func (m *mockPublisher) PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current >= len(m.responses) {
		m.current++
		return nil
	}

	resp := m.responses[m.current]
	m.current++
	return resp
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

type mockEventGetter struct {
	events          map[string]*models.Event
	err             error
	lastRetrievedID string
}

func newMockEventGetter() *mockEventGetter {
	return &mockEventGetter{
		events: make(map[string]*models.Event),
	}
}

func (m *mockEventGetter) registerEvent(event *models.Event) {
	m.events[event.ID] = event
}

func (m *mockEventGetter) clearError() {
	m.err = nil
}

func (m *mockEventGetter) RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.lastRetrievedID = eventID
	event, ok := m.events[eventID]
	if !ok {
		return nil, errors.New("event not found")
	}
	return event, nil
}

type mockLogPublisher struct {
	err        error
	deliveries []models.DeliveryEvent
}

func newMockLogPublisher(err error) *mockLogPublisher {
	return &mockLogPublisher{
		err:        err,
		deliveries: make([]models.DeliveryEvent, 0),
	}
}

func (m *mockLogPublisher) Publish(ctx context.Context, deliveryEvent models.DeliveryEvent) error {
	m.deliveries = append(m.deliveries, deliveryEvent)
	return m.err
}

type mockRetryScheduler struct {
	schedules    []string
	taskIDs      []string
	canceled     []string
	scheduleResp []error
	cancelResp   []error
	scheduleIdx  int
	cancelIdx    int
}

func newMockRetryScheduler() *mockRetryScheduler {
	return &mockRetryScheduler{
		schedules:    make([]string, 0),
		taskIDs:      make([]string, 0),
		canceled:     make([]string, 0),
		scheduleResp: make([]error, 0),
		cancelResp:   make([]error, 0),
	}
}

func (m *mockRetryScheduler) Schedule(ctx context.Context, task string, delay time.Duration, opts ...scheduler.ScheduleOption) error {
	m.schedules = append(m.schedules, task)

	// Capture the task ID by applying the option
	if len(opts) > 0 {
		options := &scheduler.ScheduleOptions{}
		opts[0](options)
		if options.ID != "" {
			m.taskIDs = append(m.taskIDs, options.ID)
		}
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

func newDeliveryMockMessage(deliveryEvent models.DeliveryEvent) (*mockMessage, *mqs.Message) {
	mock := &mockMessage{id: deliveryEvent.ID}
	body, err := json.Marshal(deliveryEvent)
	if err != nil {
		panic(err)
	}
	return mock, &mqs.Message{
		QueueMessage: mock,
		Body:         body,
	}
}

func newMockMessage(id string) *mqs.Message {
	mock := &mockMessage{id: id}
	return &mqs.Message{
		QueueMessage: mock,
		Body:         nil,
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
