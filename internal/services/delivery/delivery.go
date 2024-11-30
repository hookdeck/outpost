package delivery

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
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

	cleanupFuncs := []func(){}

	redisClient, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return nil, err
	}

	chDB, err := clickhouse.New(cfg.ClickHouse)
	if err != nil {
		return nil, err
	}

	logMQ := logmq.New(logmq.WithQueue(cfg.LogQueueConfig))
	cleanupLogMQ, err := logMQ.Init(ctx)
	if err != nil {
		return nil, err
	}
	cleanupFuncs = append(cleanupFuncs, cleanupLogMQ)

	if handler == nil {
		registry := destregistry.NewRegistry()
		destregistrydefault.RegisterDefault(registry)
		var eventTracer eventtracer.EventTracer
		if cfg.OpenTelemetry == nil {
			eventTracer = eventtracer.NewNoopEventTracer()
		} else {
			eventTracer = eventtracer.NewEventTracer()
		}
		entityStore := models.NewEntityStore(
			redisClient,
			models.NewAESCipher(cfg.EncryptionSecret),
			cfg.Topics,
		)
		logStore := models.NewLogStore(chDB)
		deliveryMQ := deliverymq.New(deliverymq.WithQueue(cfg.DeliveryQueueConfig))
		cleanupDeliveryMQ, err := deliveryMQ.Init(ctx)
		if err != nil {
			return nil, err
		}
		cleanupFuncs = append(cleanupFuncs, cleanupDeliveryMQ)
		retryScheduler := deliverymq.NewRetryScheduler(deliveryMQ, cfg.Redis)
		if err := retryScheduler.Init(ctx); err != nil {
			return nil, err
		}
		cleanupFuncs = append(cleanupFuncs, func() {
			retryScheduler.Shutdown()
		})

		handler = deliverymq.NewMessageHandler(
			logger,
			redisClient,
			logMQ,
			entityStore,
			logStore,
			registry,
			eventTracer,
			retryScheduler,
			&backoff.ExponentialBackoff{
				Interval: time.Duration(cfg.RetryIntervalSeconds) * time.Second,
				Base:     2,
			},
			cfg.RetryMaxCount,
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
		for _, cleanup := range cleanupFuncs {
			cleanup()
		}
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
