package log

import (
	"context"
	"errors"
	"sync"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/logmq"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type consumerOptions struct {
	concurreny int
}

type LogService struct {
	consumerOptions *consumerOptions
	logger          *otelzap.Logger
	redisClient     *redis.Client
	logMQ           *logmq.LogMQ
	handler         consumer.MessageHandler
}

func NewService(ctx context.Context,
	wg *sync.WaitGroup,
	cfg *config.Config,
	logger *otelzap.Logger,
	handler consumer.MessageHandler,
) (*LogService, error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		logger.Ctx(ctx).Info("service shutdown", zap.String("service", "log"))
	}()

	redisClient, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return nil, err
	}

	if handler == nil {
		handler = logmq.NewMessageHandler(logger)
	}

	service := &LogService{}
	service.logger = logger
	service.redisClient = redisClient
	service.logMQ = logmq.New(logmq.WithQueue(cfg.LogQueueConfig))
	service.consumerOptions = &consumerOptions{
		concurreny: cfg.DeliveryMaxConcurrency,
	}
	service.handler = handler

	return service, nil
}

func (s *LogService) Run(ctx context.Context) error {
	logger := s.logger.Ctx(ctx)
	logger.Info("start service", zap.String("service", "log"))

	subscription, err := s.logMQ.Subscribe(ctx)
	if err != nil {
		logger.Error("failed to susbcribe to log events", zap.Error(err))
		return err
	}

	csm := consumer.New(subscription, s.handler,
		consumer.WithConcurrency(s.consumerOptions.concurreny),
		consumer.WithName("logmq"),
	)
	if err := csm.Run(ctx); !errors.Is(err, ctx.Err()) {
		return err
	}
	return nil
}
