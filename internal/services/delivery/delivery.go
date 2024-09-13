package delivery

import (
	"context"
	"sync"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	_ "gocloud.dev/pubsub/mempubsub"
)

type DeliveryService struct {
	logger       *otelzap.Logger
	redisClient  *redis.Client
	eventHandler EventHandler
}

func NewService(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *otelzap.Logger, handler EventHandler) (*DeliveryService, error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		logger.Ctx(ctx).Info("service shutdown", zap.String("service", "delivery"))
	}()

	redisClient, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return nil, err
	}

	if handler == nil {
		handler = &eventHandler{
			logger:           logger,
			redisClient:      redisClient,
			destinationModel: models.NewDestinationModel(),
		}
	}

	service := &DeliveryService{
		logger:       logger,
		redisClient:  redisClient,
		eventHandler: handler,
	}

	return service, nil
}

func (s *DeliveryService) Run(ctx context.Context) error {
	s.logger.Ctx(ctx).Info("start service", zap.String("service", "delivery"))

	ingestor := ingest.New(s.logger, nil)
	closeDeliveryTopic, err := ingestor.OpenDeliveryTopic(ctx)
	defer closeDeliveryTopic()

	subscription, err := ingestor.OpenSubscriptionDeliveryTopic(ctx)
	if err != nil {
		s.logger.Ctx(ctx).Error("failed to open subscription", zap.Error(err))
		return err
	}

	for {
		msg, err := subscription.Receive(ctx)
		logger := s.logger.Ctx(ctx)
		if err != nil {
			if err == context.Canceled {
				logger.Info("context canceled")
				break
			}
			// Errors from Receive indicate that Receive will no longer succeed.
			logger.Error("failed to receive message", zap.Error(err))
			break
		}

		// Do work based on the message.
		// TODO: use goroutine to process messages concurrently.
		// ref: https://gocloud.dev/howto/pubsub/subscribe/#receiving
		logger.Info("received message", zap.String("message", string(msg.Body)))
		event := ingest.Event{}
		err = event.FromMessage(msg)
		if err != nil {
			logger.Error("failed to unmarshal event", zap.Error(err))
			msg.Nack()
			return err
		}
		err = s.eventHandler.Handle(ctx, event)
		if err != nil {
			logger.Error("failed to handle message", zap.Error(err))
			msg.Nack()
			return err
		}
		msg.Ack()
	}

	return nil
}
