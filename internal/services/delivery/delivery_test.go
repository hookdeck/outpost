package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupTestDeliveryService(t *testing.T,
	handler consumer.MessageHandler,
	deliveryMQ *deliverymq.DeliveryMQ,
) *DeliveryService {
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	service := &DeliveryService{
		Logger:      logger,
		RedisClient: redisClient,
		Handler:     handler,
		DeliveryMQ:  deliveryMQ,
		consumerOptions: &consumerOptions{
			concurreny: 1,
		},
	}
	return service
}

func TestDeliveryService(t *testing.T) {
	t.Parallel()

	t.Run("should run without error", func(t *testing.T) {
		t.Parallel()

		deliveryMQ := deliverymq.New(deliverymq.WithQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}))
		cleanup, err := deliveryMQ.Init(context.Background())
		require.Nil(t, err)
		defer cleanup()

		service := setupTestDeliveryService(t, nil, deliveryMQ)

		errchan := make(chan error)
		context, cancel := context.WithTimeout(context.Background(), time.Second/10)
		defer cancel()

		go func() {
			errchan <- service.Run(context)
		}()

		err = <-errchan

		assert.Nil(t, err)
	})

	t.Run("should subscribe to ingest events", func(t *testing.T) {
		t.Parallel()

		// Arrange
		deliveryMQ := deliverymq.New(deliverymq.WithQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}))
		cleanup, err := deliveryMQ.Init(context.Background())
		require.Nil(t, err)
		defer cleanup()

		handler := new(MockEventHandler)
		handler.On(
			"Handle",
			mock.MatchedBy(func(ctx context.Context) bool { return true }),
			mock.MatchedBy(func(i *mqs.Message) bool { return true }),
		).Return(nil)
		service := setupTestDeliveryService(t, handler, deliveryMQ)

		errchan := make(chan error)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second/2)
		defer cancel()

		go func() {
			errchan <- service.Run(ctx)
		}()

		// Act
		time.Sleep(time.Second / 5) // wait for service to start
		expectedID := uuid.New().String()
		deliveryMQ.Publish(ctx, models.DeliveryEvent{
			Event:       models.Event{ID: expectedID},
			Destination: models.Destination{},
		})

		// Assert
		// wait til service has stopped
		err = <-errchan
		require.Nil(t, err)

		handler.AssertCalled(t, "Handle",
			mock.MatchedBy(func(ctx context.Context) bool { return true }),
			mock.MatchedBy(func(i interface{}) bool {
				e, ok := i.(*mqs.Message)
				if !ok {
					return false
				}
				event := models.DeliveryEvent{}
				if err := event.FromMessage(e); err != nil {
					return false
				}
				return expectedID == event.Event.ID
			}),
		)
	})
}

type MockEventHandler struct {
	mock.Mock
}

var _ consumer.MessageHandler = (*MockEventHandler)(nil)

func (h *MockEventHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	args := h.Called(ctx, msg)
	return args.Error(0)
}
