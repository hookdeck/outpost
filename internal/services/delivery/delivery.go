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
	ingestor     *ingest.Ingestor
	eventHandler EventHandler
}

func NewService(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *otelzap.Logger, ingestor *ingest.Ingestor, handler EventHandler) (*DeliveryService, error) {
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
			logger:      logger,
			redisClient: redisClient,
			destinationModel: models.NewDestinationModel(
				models.DestinationModelWithCipher(models.NewAESCipher(cfg.EncryptionSecret)),
			),
		}
	}

	service := &DeliveryService{
		logger:       logger,
		redisClient:  redisClient,
		eventHandler: handler,
		ingestor:     ingestor,
	}

	return service, nil
}

func (s *DeliveryService) Run(ctx context.Context) error {
	s.logger.Ctx(ctx).Info("start service", zap.String("service", "delivery"))

	subscription, err := s.ingestor.Subscribe(ctx)
	if err != nil {
		s.logger.Ctx(ctx).Error("failed to susbcribe to ingestion events", zap.Error(err))
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
		event := ingest.Event{}
		err = event.FromMessage(msg)
		if err != nil {
			logger.Error("failed to parse message", zap.Error(err))
			msg.Nack()
			continue
		}

		// Do work based on the message.
		// TODO: use goroutine to process messages concurrently.
		// ref: https://gocloud.dev/howto/pubsub/subscribe/#receiving
		logger.Info("received event", zap.String("event", string(event.ID)))
		err = s.eventHandler.Handle(ctx, event)
		if err != nil {
			logger.Error("failed to handle message", zap.Error(err))
			msg.Nack()
			continue
		}
		msg.Ack()
	}

	return nil
}
