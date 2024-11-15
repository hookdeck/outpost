package models_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
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

func TestDestinationTopics_Validate(t *testing.T) {
	t.Parallel()

	type testCase struct {
		topics    models.Topics
		validated bool
	}

	testCases := []testCase{
		{
			topics:    []string{"user.created"},
			validated: true,
		},
		{
			topics:    []string{"user.created", "user.updated"},
			validated: true,
		},
		{
			topics:    []string{"*"},
			validated: true,
		},
		{
			topics:    []string{"*", "user.created"},
			validated: false,
		},
		{
			topics:    []string{"user.invalid"},
			validated: false,
		},
		{
			topics:    []string{"user.created", "user.invalid"},
			validated: false,
		},
		{
			topics:    []string{},
			validated: false,
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("validate topics %v", tc.topics), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.validated, tc.topics.Validate(testutil.TestTopics) == nil)
		})
	}
}
