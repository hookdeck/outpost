package testutil

import (
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
)

// ============================== Mock Event ==============================

var EventFactory = &mockEventFactory{}

type mockEventFactory struct {
}

func (f *mockEventFactory) Any(opts ...func(*models.Event)) models.Event {
	event := models.Event{
		ID:               idgen.Event(),
		TenantID:         "test-tenant",
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

// ============================== Mock Attempt ==============================

var AttemptFactory = &mockAttemptFactory{}

type mockAttemptFactory struct {
}

func (f *mockAttemptFactory) Any(opts ...func(*models.Attempt)) models.Attempt {
	attempt := models.Attempt{
		ID:            idgen.Attempt(),
		TenantID:      "test-tenant",
		EventID:       idgen.Event(),
		DestinationID: idgen.Destination(),
		AttemptNumber: 1,
		Manual:        false,
		Status:        "success",
		Time:          time.Now(),
	}

	for _, opt := range opts {
		opt(&attempt)
	}

	return attempt
}

func (f *mockAttemptFactory) AnyPointer(opts ...func(*models.Attempt)) *models.Attempt {
	attempt := f.Any(opts...)
	return &attempt
}

func (f *mockAttemptFactory) WithID(id string) func(*models.Attempt) {
	return func(attempt *models.Attempt) {
		attempt.ID = id
	}
}

func (f *mockAttemptFactory) WithTenantID(tenantID string) func(*models.Attempt) {
	return func(attempt *models.Attempt) {
		attempt.TenantID = tenantID
	}
}

func (f *mockAttemptFactory) WithAttemptNumber(attemptNumber int) func(*models.Attempt) {
	return func(attempt *models.Attempt) {
		attempt.AttemptNumber = attemptNumber
	}
}

func (f *mockAttemptFactory) WithManual(manual bool) func(*models.Attempt) {
	return func(attempt *models.Attempt) {
		attempt.Manual = manual
	}
}

func (f *mockAttemptFactory) WithEventID(eventID string) func(*models.Attempt) {
	return func(attempt *models.Attempt) {
		attempt.EventID = eventID
	}
}

func (f *mockAttemptFactory) WithDestinationID(destinationID string) func(*models.Attempt) {
	return func(attempt *models.Attempt) {
		attempt.DestinationID = destinationID
	}
}

func (f *mockAttemptFactory) WithStatus(status string) func(*models.Attempt) {
	return func(attempt *models.Attempt) {
		attempt.Status = status
	}
}

func (f *mockAttemptFactory) WithCode(code string) func(*models.Attempt) {
	return func(attempt *models.Attempt) {
		attempt.Code = code
	}
}

func (f *mockAttemptFactory) WithTime(time time.Time) func(*models.Attempt) {
	return func(attempt *models.Attempt) {
		attempt.Time = time
	}
}
