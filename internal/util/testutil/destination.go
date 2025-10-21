package testutil

import (
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
)

// ============================== Mock Destination ==============================

var DestinationFactory = &mockDestinationFactory{}

type mockDestinationFactory struct {
}

func (f *mockDestinationFactory) Any(opts ...func(*models.Destination)) models.Destination {
	destination := models.Destination{
		ID:          idgen.Destination(),
		Type:        "webhook",
		Topics:      []string{"*"},
		Config:      map[string]string{"url": "http://host.docker.internal:4444"},
		Credentials: map[string]string{},
		CreatedAt:   time.Now(),
		TenantID:    "test-tenant",
		DisabledAt:  nil,
	}

	for _, opt := range opts {
		opt(&destination)
	}

	return destination
}

func (f *mockDestinationFactory) WithID(id string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.ID = id
	}
}

func (f *mockDestinationFactory) WithTenantID(tenantID string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.TenantID = tenantID
	}
}

func (f *mockDestinationFactory) WithType(destinationType string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.Type = destinationType
	}
}

func (f *mockDestinationFactory) WithTopics(topics []string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.Topics = topics
	}
}

func (f *mockDestinationFactory) WithConfig(config map[string]string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.Config = config
	}
}

func (f *mockDestinationFactory) WithCredentials(credentials map[string]string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.Credentials = credentials
	}
}

func (f *mockDestinationFactory) WithCreatedAt(createdAt time.Time) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.CreatedAt = createdAt
	}
}

func (f *mockDestinationFactory) WithDisabledAt(disabledAt time.Time) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.DisabledAt = &disabledAt
	}
}

func (f *mockDestinationFactory) WithDeliveryMetadata(deliveryMetadata map[string]string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.DeliveryMetadata = deliveryMetadata
	}
}

func (f *mockDestinationFactory) WithMetadata(metadata map[string]string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.Metadata = metadata
	}
}
