package delivery

import (
	"context"
	"sync"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	_ "gocloud.dev/pubsub/mempubsub"
)

type DeliveryService struct {
	Logger       *otelzap.Logger
	RedisClient  *redis.Client
	DeliveryMQ   *deliverymq.DeliveryMQ
	EventHandler deliverymq.EventHandler
}

func NewService(ctx context.Context,
	wg *sync.WaitGroup,
	cfg *config.Config,
	logger *otelzap.Logger,
	handler deliverymq.EventHandler, // accept an EventHandler interface for testing purposes
) (*DeliveryService, error) {
	wg.Add(1)

	deliveryMQ := deliverymq.New(deliverymq.WithQueue(cfg.DeliveryQueueConfig))
	cleanupDeliveryMQ, err := deliveryMQ.Init(ctx)
	if err != nil {
		return nil, err
	}

	redisClient, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return nil, err
	}

	if handler == nil {
		destinationModel := models.NewDestinationModel(
			models.DestinationModelWithCipher(models.NewAESCipher(cfg.EncryptionSecret)),
		)
		handler = deliverymq.NewEventHandler(logger, redisClient, destinationModel)
	}

	service := &DeliveryService{
		Logger:       logger,
		RedisClient:  redisClient,
		EventHandler: handler,
		DeliveryMQ:   deliveryMQ,
	}

	go func() {
		defer wg.Done()
		<-ctx.Done()
		logger.Ctx(ctx).Info("shutting down", zap.String("service", "delivery"))
		cleanupDeliveryMQ()
		logger.Ctx(ctx).Info("service shutdown", zap.String("service", "delivery"))
	}()

	return service, nil
}

func (s *DeliveryService) Run(ctx context.Context) error {
	s.Logger.Ctx(ctx).Info("start service", zap.String("service", "delivery"))

	subscription, err := s.DeliveryMQ.Subscribe(ctx)
	if err != nil {
		s.Logger.Ctx(ctx).Error("failed to susbcribe to ingestion events", zap.Error(err))
		return err
	}

	for {
		msg, err := subscription.Receive(ctx)
		logger := s.Logger.Ctx(ctx)
		if err != nil {
			if err == context.Canceled {
				logger.Info("context canceled")
				break
			}
			// Errors from Receive indicate that Receive will no longer succeed.
			logger.Error("failed to receive message", zap.Error(err))
			break
		}
		deliveryEvent := models.DeliveryEvent{}
		err = deliveryEvent.FromMessage(msg)
		if err != nil {
			logger.Error("failed to parse message", zap.Error(err))
			msg.Nack()
			continue
		}

		// Do work based on the message.
		// TODO: use goroutine to process messages concurrently.
		// ref: https://gocloud.dev/howto/pubsub/subscribe/#receiving
		logger.Info("received delivery event", zap.String("delivery_event.event", string(deliveryEvent.Event.ID)))
		err = s.EventHandler.Handle(ctx, deliveryEvent)
		if err != nil {
			logger.Error("failed to handle message", zap.Error(err))
			msg.Nack()
			continue
		}
		msg.Ack()
	}

	return nil
}
