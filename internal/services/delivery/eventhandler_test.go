package delivery_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/services/delivery"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestEventHandler(t *testing.T) {
	t.Parallel()

	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	destinationModel := models.NewDestinationModel()
	eventHandler := delivery.NewEventHandler(logger, redisClient, destinationModel)

	// TODO: Question: Should we return error here?
	t.Run("should not return error when there's no tenant or destination", func(t *testing.T) {
		event := ingest.Event{
			ID:               uuid.New().String(),
			TenantID:         uuid.New().String(),
			DestinationID:    uuid.New().String(),
			Topic:            "test",
			EligibleForRetry: true,
			Time:             time.Now(),
			Metadata:         map[string]string{},
			Data:             map[string]interface{}{},
		}
		err := eventHandler.Handle(context.Background(), event)
		assert.Nil(t, err)
	})

	// TODO: add more tests
}
