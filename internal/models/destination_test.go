package models_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestDestination_Validate(t *testing.T) {
	t.Parallel()

	t.Run("validates valid", func(t *testing.T) {
		t.Parallel()
		destination := models.Destination{
			ID:          uuid.New().String(),
			Type:        "webhooks",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{"url": "https://example.com"},
			Credentials: map[string]string{},
			CreatedAt:   time.Now(),
			TenantID:    uuid.New().String(),
			DisabledAt:  nil,
		}
		assert.Nil(t, destination.Validate(context.Background()))
	})

	t.Run("validates invalid type", func(t *testing.T) {
		t.Parallel()
		destination := models.Destination{
			ID:          uuid.New().String(),
			Type:        "invalid",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{},
			Credentials: map[string]string{},
			CreatedAt:   time.Now(),
			TenantID:    uuid.New().String(),
			DisabledAt:  nil,
		}
		assert.ErrorContains(t, destination.Validate(context.Background()), "invalid destination type")
	})

	t.Run("validates invalid config", func(t *testing.T) {
		t.Parallel()
		destination := models.Destination{
			ID:          uuid.New().String(),
			Type:        "webhooks",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{},
			Credentials: map[string]string{},
			CreatedAt:   time.Now(),
			TenantID:    uuid.New().String(),
			DisabledAt:  nil,
		}
		assert.ErrorContains(t,
			destination.Validate(context.Background()),
			"url is required for webhook destination config",
		)
	})

	t.Run("validates invalid credentials", func(t *testing.T) {
		t.Parallel()
		destination := models.Destination{
			ID:     uuid.New().String(),
			Type:   "rabbitmq",
			Topics: []string{"user.created", "user.updated"},
			Config: map[string]string{
				"server_url": "localhost:5672",
				"exchange":   "events",
			},
			Credentials: map[string]string{
				"username":    "guest",
				"notpassword": "guest",
			},
			CreatedAt:  time.Now(),
			TenantID:   uuid.New().String(),
			DisabledAt: nil,
		}
		assert.ErrorContains(t,
			destination.Validate(context.Background()),
			"password is required for rabbitmq destination credentials",
		)
	})
}

// ============================== Mock Destination ==============================

var mockDestinationFactory = &MockDestinationFactory{}

type MockDestinationFactory struct {
}

func (f *MockDestinationFactory) Any(opts ...func(*models.Destination)) models.Destination {
	destination := models.Destination{
		ID:          uuid.New().String(),
		Type:        "webhooks",
		Topics:      []string{"user.created", "user.updated"},
		Config:      map[string]string{"url": "https://example.com"},
		Credentials: map[string]string{},
		CreatedAt:   time.Now(),
		TenantID:    uuid.New().String(),
		DisabledAt:  nil,
	}

	for _, opt := range opts {
		opt(&destination)
	}

	return destination
}

func (f *MockDestinationFactory) WithID(id string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.ID = id
	}
}

func (f *MockDestinationFactory) WithTenantID(tenantID string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.TenantID = tenantID
	}
}

func (f *MockDestinationFactory) WithType(destinationType string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.Type = destinationType
	}
}

func (f *MockDestinationFactory) WithTopics(topics []string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.Topics = topics
	}
}

func (f *MockDestinationFactory) WithConfig(config map[string]string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.Config = config
	}
}

func (f *MockDestinationFactory) WithCredentials(credentials map[string]string) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.Credentials = credentials
	}
}

func (f *MockDestinationFactory) WithCreatedAt(createdAt time.Time) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.CreatedAt = createdAt
	}
}

func (f *MockDestinationFactory) WithDisabledAt(disabledAt time.Time) func(*models.Destination) {
	return func(destination *models.Destination) {
		destination.DisabledAt = &disabledAt
	}
}
