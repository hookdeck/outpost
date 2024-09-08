package delivery

import (
	"context"
	"sync"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/deliverer"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	_ "gocloud.dev/pubsub/mempubsub"
)

type DeliveryService struct {
	logger      *otelzap.Logger
	redisClient *redis.Client
}

func NewService(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *otelzap.Logger) (*DeliveryService, error) {
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

	service := &DeliveryService{
		logger:      logger,
		redisClient: redisClient,
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

	destinationModel := models.NewDestinationModel()

	for {
		msg, err := subscription.Receive(ctx)
		if err != nil {
			// Errors from Receive indicate that Receive will no longer succeed.
			s.logger.Ctx(ctx).Error("failed to receive message", zap.Error(err))
			break
		}

		// Do work based on the message.
		// TODO: use goroutine to process messages concurrently.
		// ref: https://gocloud.dev/howto/pubsub/subscribe/#receiving

		event := ingest.Event{}
		err = event.FromMessage(msg)
		if err != nil {
			s.logger.Ctx(ctx).Error("failed to unmarshal event", zap.Error(err))
			msg.Nack()
			continue
		}

		destinations, err := destinationModel.List(ctx, s.redisClient, event.TenantID)
		if err != nil {
			s.logger.Ctx(ctx).Error("failed to list destinations", zap.Error(err))
			msg.Nack()
			continue
		}
		destinations = models.FilterTopics(destinations, event.Topic)

		// TODO: handle via goroutine or MQ.
		for _, destination := range destinations {
			deliverer.New(s.logger).Deliver(ctx, &destination, &event)
		}

		msg.Ack()
	}

	return nil
}
