package deliverymq_test

import (
	"context"
	"testing"

	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestDeliveryMQEventHandler(t *testing.T) {
	t.Parallel()

	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	destinationModel := models.NewDestinationModel()
	eventHandler := deliverymq.NewEventHandler(logger, redisClient, destinationModel)

	// TODO: add tests

	t.Run("TODO", func(t *testing.T) {
		t.Parallel()
		assert.NotNil(t, eventHandler.Handle(context.Background(), models.DeliveryEvent{}))
	})
}
