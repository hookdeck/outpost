package deliverymq_test

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	mqs "github.com/hookdeck/outpost/internal/mqs"
)

type mockPublisher struct {
	responses []error
	current   int
}

func newMockPublisher(responses []error) *mockPublisher {
	return &mockPublisher{responses: responses}
}

func (m *mockPublisher) PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) error {
	defer func() { m.current++ }()
	if m.current >= len(m.responses) {
		return nil
	}
	return m.responses[m.current]
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
	err error
}

func newMockLogPublisher(err error) *mockLogPublisher {
	return &mockLogPublisher{err: err}
}

func (m *mockLogPublisher) Publish(ctx context.Context, deliveryEvent models.DeliveryEvent) error {
	return m.err
}

type mockRetryScheduler struct {
	schedules []string
}

func newMockRetryScheduler() *mockRetryScheduler {
	return &mockRetryScheduler{schedules: make([]string, 0)}
}

func (m *mockRetryScheduler) Schedule(ctx context.Context, message string, delay time.Duration) error {
	m.schedules = append(m.schedules, message)
	return nil
}

func (m *mockRetryScheduler) Monitor(ctx context.Context) error { return nil }

func (m *mockRetryScheduler) Init(ctx context.Context) error { return nil }

func (m *mockRetryScheduler) Shutdown() error { return nil }

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
