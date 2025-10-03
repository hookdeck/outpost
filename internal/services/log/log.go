package log

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/mikestefanello/batcher"
	"go.uber.org/zap"
)

type consumerOptions struct {
	concurreny int
}

type LogService struct {
	cleanupFuncs    []func(context.Context, *logging.LoggerWithCtx)
	consumerOptions *consumerOptions
	logger          *logging.Logger
	redisClient     redis.Cmdable
	logMQ           *logmq.LogMQ
	handler         consumer.MessageHandler
}

func NewService(ctx context.Context,
	wg *sync.WaitGroup,
	cfg *config.Config,
	logger *logging.Logger,
	handler consumer.MessageHandler,
) (*LogService, error) {
	wg.Add(1)
	var cleanupFuncs []func(context.Context, *logging.LoggerWithCtx)

	redisClient, err := redis.New(ctx, cfg.Redis.ToConfig())
	if err != nil {
		return nil, err
	}

	var eventBatcher *batcher.Batcher[*models.Event]
	var deliveryBatcher *batcher.Batcher[*models.Delivery]
	if handler == nil {
		logstoreDriverOpts, err := logstore.MakeDriverOpts(logstore.Config{
			// ClickHouse: cfg.ClickHouse.ToConfig(),
			Postgres: &cfg.PostgresURL,
		})
		if err != nil {
			return nil, err
		}
		cleanupFuncs = append(cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
			logstoreDriverOpts.Close()
		})

		logStore, err := logstore.NewLogStore(ctx, logstoreDriverOpts)
		if err != nil {
			return nil, err
		}

		batcherCfg := batcherConfig{
			ItemCountThreshold: cfg.LogBatchSize,
			DelayThreshold:     time.Duration(cfg.LogBatchThresholdSeconds) * time.Second,
		}
		batcher, err := makeBatcher(ctx, logger, logStore, batcherCfg)
		if err != nil {
			return nil, err
		}

		handler = logmq.NewMessageHandler(logger, &handlerBatcherImpl{batcher: batcher})
	}
	cleanupFuncs = append(cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		if eventBatcher != nil {
			eventBatcher.Shutdown()
		}
		if deliveryBatcher != nil {
			deliveryBatcher.Shutdown()
		}
	})

	logQueueConfig, err := cfg.MQs.ToQueueConfig(ctx, "logmq")
	if err != nil {
		return nil, err
	}

	service := &LogService{}
	service.logger = logger
	service.redisClient = redisClient
	service.logMQ = logmq.New(logmq.WithQueue(logQueueConfig))
	service.consumerOptions = &consumerOptions{
		concurreny: cfg.DeliveryMaxConcurrency,
	}
	service.handler = handler
	service.cleanupFuncs = cleanupFuncs

	go func() {
		defer wg.Done()
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		service.Shutdown(shutdownCtx)
	}()

	return service, nil
}

func (s *LogService) Run(ctx context.Context) error {
	logger := s.logger.Ctx(ctx)
	logger.Info("start service", zap.String("service", "log"))

	subscription, err := s.logMQ.Subscribe(ctx)
	if err != nil {
		logger.Error("failed to susbcribe to logmq", zap.Error(err))
		return err
	}

	csm := consumer.New(subscription, s.handler,
		consumer.WithConcurrency(s.consumerOptions.concurreny),
		consumer.WithName("logmq"),
	)
	if err := csm.Run(ctx); !errors.Is(err, ctx.Err()) {
		logger.Error("failed to run logmq consumer", zap.Error(err))
		return err
	}
	return nil
}

func (s *LogService) Shutdown(ctx context.Context) {
	logger := s.logger.Ctx(ctx)
	logger.Info("service shutting down", zap.String("service", "log"))
	for _, cleanupFunc := range s.cleanupFuncs {
		cleanupFunc(ctx, &logger)
	}
	logger.Info("service shutdown", zap.String("service", "log"))
}

type batcherConfig struct {
	ItemCountThreshold int
	DelayThreshold     time.Duration
}

func makeBatcher(ctx context.Context, logger *logging.Logger, logStore logstore.LogStore, batcherCfg batcherConfig) (*batcher.Batcher[*mqs.Message], error) {
	b, err := batcher.NewBatcher(batcher.Config[*mqs.Message]{
		GroupCountThreshold: 2,
		ItemCountThreshold:  batcherCfg.ItemCountThreshold,
		DelayThreshold:      batcherCfg.DelayThreshold,
		NumGoroutines:       1,
		Processor: func(_ string, msgs []*mqs.Message) {
			logger := logger.Ctx(ctx)
			logger.Info("processing batch", zap.Int("message_count", len(msgs)))

			nackAll := func() {
				for _, msg := range msgs {
					msg.Nack()
				}
			}

			deliveryEvents := make([]*models.DeliveryEvent, 0, len(msgs))
			for _, msg := range msgs {
				deliveryEvent := models.DeliveryEvent{}
				if err := deliveryEvent.FromMessage(msg); err != nil {
					// TODO: consider nacking this individual message only
					logger.Error("failed to parse delivery event",
						zap.Error(err),
						zap.String("message_id", msg.LoggableID))
					nackAll()
					return
				}
				deliveryEvents = append(deliveryEvents, &deliveryEvent)
			}

			if err := logStore.InsertManyDeliveryEvent(ctx, deliveryEvents); err != nil {
				logger.Error("failed to insert delivery events",
					zap.Error(err),
					zap.Int("count", len(deliveryEvents)))
				nackAll()
				return
			}

			logger.Info("batch processed successfully", zap.Int("count", len(msgs)))

			for _, msg := range msgs {
				msg.Ack()
			}
		},
	})
	if err != nil {
		logger.Ctx(ctx).Error("failed to create batcher", zap.Error(err))
		return nil, err
	}
	return b, nil
}

type handlerBatcherImpl struct {
	batcher *batcher.Batcher[*mqs.Message]
}

func (b *handlerBatcherImpl) Add(ctx context.Context, msg *mqs.Message) error {
	b.batcher.Add("", msg)
	return nil
}
