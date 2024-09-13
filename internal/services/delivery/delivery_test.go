package delivery_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/services/delivery"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	r "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

func setupTestDeliveryService(t *testing.T, handler delivery.EventHandler) (*delivery.DeliveryService, error, *otelzap.Logger, *config.Config) {
	logger := testutil.CreateTestLogger(t)
	redisConfig := testutil.CreateTestRedisConfig(t)
	config := config.Config{Redis: redisConfig}
	wg := sync.WaitGroup{}
	service, err := delivery.NewService(context.Background(), &wg, &config, logger, handler)
	return service, err, logger, &config
}

func TestDeliveryService(t *testing.T) {
	t.Parallel()

	t.Run("should run without error", func(t *testing.T) {
		t.Parallel()

		service, err, _, _ := setupTestDeliveryService(t, nil)
		assert.Nil(t, err)

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

		handler := new(MockEventHandler)
		handler.On(
			"Handle",
			mock.MatchedBy(func(ctx context.Context) bool { return true }),
			mock.MatchedBy(func(i ingest.Event) bool { return true }),
		).Return(nil)
		service, err, logger, config := setupTestDeliveryService(t, handler)
		if err != nil {
			t.Fatal(err)
		}

		errchan := make(chan error)
		ctx, cancel := context.WithCancel(context.Background())

		redisClient := r.NewClient(&r.Options{
			Addr:     fmt.Sprintf("%s:%d", config.Redis.Host, config.Redis.Port),
			Password: config.Redis.Password,
			DB:       config.Redis.Database,
		})
		ingestor := ingest.New(logger, redisClient)
		closeDeliveryTopic, err := ingestor.OpenDeliveryTopic(ctx)
		defer closeDeliveryTopic()

		go func() {
			errchan <- service.Run(ctx)
		}()

		go func() {
			time.Sleep(time.Second / 2)
			cancel()
		}()

		// Act
		ingestor.Ingest(ctx, event)

		// Assert
		// wait til service has stopped
		err = <-errchan
		if err != nil {
			t.Fatal(err)
		}
		handler.AssertCalled(t, "Handle",
			mock.MatchedBy(func(ctx context.Context) bool { return true }),
			mock.MatchedBy(func(i interface{}) bool {
				e, ok := i.(ingest.Event)
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

func (h *MockEventHandler) Handle(ctx context.Context, event ingest.Event) error {
	args := h.Called(ctx, event)
	return args.Error(0)
}
