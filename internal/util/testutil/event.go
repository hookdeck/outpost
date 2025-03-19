package testutil

import (
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/models"
)

// ============================== Mock Event ==============================

var EventFactory = &mockEventFactory{}

type mockEventFactory struct {
}

func (f *mockEventFactory) Any(opts ...func(*models.Event)) models.Event {
	event := models.Event{
		ID:               uuid.New().String(),
		TenantID:         uuid.New().String(),
		DestinationID:    "",
		Topic:            TestTopics[0],
		EligibleForRetry: true,
		Time:             time.Now(),
		Metadata: map[string]string{
			"metadatakey": "metadatavalue",
		},
		Data: map[string]interface{}{
			"mykey": "myvalue",
		},
	}

	for _, opt := range opts {
		opt(&event)
	}

	return event
}

func (f *mockEventFactory) AnyPointer(opts ...func(*models.Event)) *models.Event {
	event := f.Any(opts...)
	return &event
}

func (f *mockEventFactory) WithID(id string) func(*models.Event) {
	return func(event *models.Event) {
		event.ID = id
	}
}

func (f *mockEventFactory) WithTenantID(tenantID string) func(*models.Event) {
	return func(event *models.Event) {
		event.TenantID = tenantID
	}
}

func (f *mockEventFactory) WithDestinationID(destinationID string) func(*models.Event) {
	return func(event *models.Event) {
		event.DestinationID = destinationID
	}
}

func (f *mockEventFactory) WithTopic(topic string) func(*models.Event) {
	return func(event *models.Event) {
		event.Topic = topic
	}
}

func (f *mockEventFactory) WithEligibleForRetry(eligibleForRetry bool) func(*models.Event) {
	return func(event *models.Event) {
		event.EligibleForRetry = eligibleForRetry
	}
}

func (f *mockEventFactory) WithTime(time time.Time) func(*models.Event) {
	return func(event *models.Event) {
		event.Time = time
	}
}

func (f *mockEventFactory) WithStatus(status string) func(*models.Event) {
	return func(event *models.Event) {
		event.Status = status
	}
}

func (f *mockEventFactory) WithMetadata(metadata map[string]string) func(*models.Event) {
	return func(event *models.Event) {
		event.Metadata = metadata
	}
}

func (f *mockEventFactory) WithData(data map[string]interface{}) func(*models.Event) {
	return func(event *models.Event) {
		event.Data = data
	}
}

// ============================== Mock Delivery ==============================

var DeliveryFactory = &mockDeliveryFactory{}

type mockDeliveryFactory struct {
}

func (f *mockDeliveryFactory) Any(opts ...func(*models.Delivery)) models.Delivery {
	delivery := models.Delivery{
		ID:              uuid.New().String(),
		DeliveryEventID: uuid.New().String(),
		EventID:         uuid.New().String(),
		DestinationID:   uuid.New().String(),
		Status:          "success",
		Time:            time.Now(),
	}

	for _, opt := range opts {
		opt(&delivery)
	}

	return delivery
}

func (f *mockDeliveryFactory) AnyPointer(opts ...func(*models.Delivery)) *models.Delivery {
	delivery := f.Any(opts...)
	return &delivery
}

func (f *mockDeliveryFactory) WithID(id string) func(*models.Delivery) {
	return func(delivery *models.Delivery) {
		delivery.ID = id
	}
}

func (f *mockDeliveryFactory) WithDeliveryEventID(deliveryEventID string) func(*models.Delivery) {
	return func(delivery *models.Delivery) {
		delivery.DeliveryEventID = deliveryEventID
	}
}

func (f *mockDeliveryFactory) WithEventID(eventID string) func(*models.Delivery) {
	return func(delivery *models.Delivery) {
		delivery.EventID = eventID
	}
}

func (f *mockDeliveryFactory) WithDestinationID(destinationID string) func(*models.Delivery) {
	return func(delivery *models.Delivery) {
		delivery.DestinationID = destinationID
	}
}

func (f *mockDeliveryFactory) WithStatus(status string) func(*models.Delivery) {
	return func(delivery *models.Delivery) {
		delivery.Status = status
	}
}

func (f *mockDeliveryFactory) WithTime(time time.Time) func(*models.Delivery) {
	return func(delivery *models.Delivery) {
		delivery.Time = time
	}
}
