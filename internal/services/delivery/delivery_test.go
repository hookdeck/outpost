package delivery_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/services/delivery"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupTestDeliveryService(t *testing.T, handler delivery.EventHandler, deliveryMQ *deliverymq.DeliveryMQ) (*delivery.DeliveryService, error) {
	logger := testutil.CreateTestLogger(t)
	redisConfig := testutil.CreateTestRedisConfig(t)
	config := config.Config{Redis: redisConfig}
	wg := sync.WaitGroup{}
	service, err := delivery.NewService(context.Background(), &wg, &config, logger, deliveryMQ, handler)
	return service, err
}

func TestDeliveryService(t *testing.T) {
	t.Parallel()

	t.Run("should run without error", func(t *testing.T) {
		t.Parallel()

		deliveryMQ := deliverymq.New(deliverymq.WithQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}))
		cleanup, err := deliveryMQ.Init(context.Background())
		require.Nil(t, err)
		defer cleanup()

		service, err := setupTestDeliveryService(t, nil, deliveryMQ)
		require.Nil(t, err)

		errchan := make(chan error)
		context, cancel := context.WithCancel(context.Background())

		go func() {
			errchan <- service.Run(context)
		}()

		go func() {
			time.Sleep(time.Second / 10)
			cancel()
		}()

		err = <-errchan

		assert.Nil(t, err)
	})

	t.Run("should subscribe to ingest events", func(t *testing.T) {
		t.Parallel()

		// Arrange
		event := models.Event{
			ID:               uuid.New().String(),
			TenantID:         uuid.New().String(),
			DestinationID:    uuid.New().String(),
			Topic:            "test",
			EligibleForRetry: true,
			Time:             time.Now(),
			Metadata:         map[string]string{},
			Data:             map[string]interface{}{},
		}

		deliveryMQ := deliverymq.New(deliverymq.WithQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}))
		cleanup, err := deliveryMQ.Init(context.Background())
		require.Nil(t, err)
		defer cleanup()

		handler := new(MockEventHandler)
		handler.On(
			"Handle",
			mock.MatchedBy(func(ctx context.Context) bool { return true }),
			mock.MatchedBy(func(i models.Event) bool { return true }),
		).Return(nil)
		service, err := setupTestDeliveryService(t, handler, deliveryMQ)
		require.Nil(t, err)

		errchan := make(chan error)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second/2)
		defer cancel()

		go func() {
			errchan <- service.Run(ctx)
		}()

		// Act
		time.Sleep(time.Second / 5) // wait for service to start
		deliveryMQ.Publish(ctx, event)

		// Assert
		// wait til service has stopped
		err = <-errchan
		require.Nil(t, err)

		handler.AssertCalled(t, "Handle",
			mock.MatchedBy(func(ctx context.Context) bool { return true }),
			mock.MatchedBy(func(i interface{}) bool {
				e, ok := i.(models.Event)
				if !ok {
					return false
				}
				return e.ID == event.ID
			}),
		)
	})
}

type MockEventHandler struct {
	mock.Mock
}

var _ delivery.EventHandler = (*MockEventHandler)(nil)

func (h *MockEventHandler) Handle(ctx context.Context, event models.Event) error {
	args := h.Called(ctx, event)
	return args.Error(0)
}
