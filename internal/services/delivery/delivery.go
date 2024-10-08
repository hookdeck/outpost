package delivery

import (
	"context"
	"errors"
	"sync"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/logmq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	_ "gocloud.dev/pubsub/mempubsub"
)

type consumerOptions struct {
	concurreny int
}

type DeliveryService struct {
	consumerOptions *consumerOptions
	Logger          *otelzap.Logger
	RedisClient     *redis.Client
	DeliveryMQ      *deliverymq.DeliveryMQ
	Handler         consumer.MessageHandler
}

func NewService(ctx context.Context,
	wg *sync.WaitGroup,
	cfg *config.Config,
	logger *otelzap.Logger,
	handler consumer.MessageHandler,
) (*DeliveryService, error) {
	wg.Add(1)

	redisClient, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return nil, err
	}

	logMQ := logmq.New(logmq.WithQueue(cfg.LogQueueConfig))
	cleanupLogMQ, err := logMQ.Init(ctx)
	if err != nil {
		return nil, err
	}

	if handler == nil {
		entityStore := models.NewEntityStore(
			redisClient,
			models.NewAESCipher(cfg.EncryptionSecret),
		)
		handler = deliverymq.NewMessageHandler(
			logger,
			redisClient,
			logMQ,
			entityStore,
		)
	}

	service := &DeliveryService{
		Logger:      logger,
		RedisClient: redisClient,
		Handler:     handler,
		DeliveryMQ:  deliverymq.New(deliverymq.WithQueue(cfg.DeliveryQueueConfig)),
		consumerOptions: &consumerOptions{
			concurreny: cfg.DeliveryMaxConcurrency,
		},
	}

	go func() {
		defer wg.Done()
		<-ctx.Done()
		cleanupLogMQ()
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

	csm := consumer.New(subscription, s.Handler,
		consumer.WithConcurrency(s.consumerOptions.concurreny),
		consumer.WithName("deliverymq"),
	)
	if err := csm.Run(ctx); !errors.Is(err, ctx.Err()) {
		return err
	}
	return nil
}
