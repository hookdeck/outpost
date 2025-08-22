package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/telemetry"
	"go.uber.org/zap"
)

type consumerOptions struct {
	concurreny int
}

type APIService struct {
	cleanupFuncs []func(context.Context, *logging.LoggerWithCtx)

	registry                 destregistry.Registry
	redisClient              *redis.Client
	server                   *http.Server
	logger                   *logging.Logger
	publishMQ                *publishmq.PublishMQ
	deliveryMQ               *deliverymq.DeliveryMQ
	entityStore              models.EntityStore
	eventHandler             publishmq.EventHandler
	deliverymqRetryScheduler scheduler.Scheduler
	consumerOptions          *consumerOptions
}

func NewService(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *logging.Logger, telemetry telemetry.Telemetry) (*APIService, error) {
	wg.Add(1)

	var cleanupFuncs []func(context.Context, *logging.LoggerWithCtx)

	logger.Debug("initializing destination registry")
	registry := destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: cfg.Destinations.MetadataPath,
		DeliveryTimeout:         time.Duration(cfg.DeliveryTimeoutSeconds) * time.Second,
	}, logger)
	if err := destregistrydefault.RegisterDefault(registry, cfg.Destinations.ToConfig(cfg)); err != nil {
		logger.Error("destination registry setup failed", zap.Error(err))
		return nil, err
	}

	logger.Debug("configuring delivery message queue")
	deliveryQueueConfig, err := cfg.MQs.ToQueueConfig(ctx, "deliverymq")
	if err != nil {
		logger.Error("delivery queue configuration failed", zap.Error(err))
		return nil, err
	}
	
	logger.Debug("initializing delivery MQ connection", zap.String("mq_type", cfg.MQs.GetInfraType()))
	deliveryMQ := deliverymq.New(deliverymq.WithQueue(deliveryQueueConfig))
	cleanupDeliveryMQ, err := deliveryMQ.Init(ctx)
	if err != nil {
		logger.Error("delivery MQ initialization failed", 
			zap.Error(err),
			zap.String("mq_type", cfg.MQs.GetInfraType()),
			zap.String("context", "This likely indicates Azure Service Bus connectivity issues"))
		return nil, err
	}
	cleanupFuncs = append(cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) { cleanupDeliveryMQ() })

	logger.Debug("initializing Redis client for API service")
	redisClient, err := redis.New(ctx, cfg.Redis.ToConfig())
	if err != nil {
		logger.Error("Redis client initialization failed in API service", zap.Error(err))
		return nil, err
	}

	logger.Debug("configuring log store driver")
	logStoreDriverOpts, err := logstore.MakeDriverOpts(logstore.Config{
		// ClickHouse: cfg.ClickHouse.ToConfig(),
		Postgres: &cfg.PostgresURL,
	})
	if err != nil {
		logger.Error("log store driver configuration failed", zap.Error(err))
		return nil, err
	}
	cleanupFuncs = append(cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		logStoreDriverOpts.Close()
	})
	
	logger.Debug("creating log store")
	logStore, err := logstore.NewLogStore(ctx, logStoreDriverOpts)
	if err != nil {
		logger.Error("log store creation failed", zap.Error(err))
		return nil, err
	}

	logger.Debug("setting up event tracer")
	var eventTracer eventtracer.EventTracer
	if cfg.OpenTelemetry.ToConfig() == nil {
		eventTracer = eventtracer.NewNoopEventTracer()
	} else {
		eventTracer = eventtracer.NewEventTracer()
	}
	
	logger.Debug("creating entity store with Redis client")
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher(cfg.AESEncryptionSecret)),
		models.WithAvailableTopics(cfg.Topics),
		models.WithMaxDestinationsPerTenant(cfg.MaxDestinationsPerTenant),
	)
	
	logger.Debug("creating event handler and router")
	eventHandler := publishmq.NewEventHandler(logger, redisClient, deliveryMQ, entityStore, eventTracer, cfg.Topics)
	router := NewRouter(
		RouterConfig{
			ServiceName:  cfg.OpenTelemetry.GetServiceName(),
			APIKey:       cfg.APIKey,
			JWTSecret:    cfg.APIJWTSecret,
			Topics:       cfg.Topics,
			Registry:     registry,
			PortalConfig: cfg.GetPortalConfig(),
			GinMode:      cfg.GinMode,
		},
		logger,
		redisClient,
		deliveryMQ,
		entityStore,
		logStore,
		eventHandler,
		telemetry,
	)

	// deliverymqRetryScheduler
	logger.Debug("creating delivery MQ retry scheduler")
	deliverymqRetryScheduler := deliverymq.NewRetryScheduler(deliveryMQ, cfg.Redis.ToConfig(), logger)
	logger.Debug("initializing delivery MQ retry scheduler - this may perform Redis operations")
	if err := deliverymqRetryScheduler.Init(ctx); err != nil {
		logger.Error("delivery MQ retry scheduler initialization failed", 
			zap.Error(err),
			zap.String("context", "This likely indicates Redis connectivity issues during actual operations"))
		return nil, err
	}
	cleanupFuncs = append(cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) { deliverymqRetryScheduler.Shutdown() })

	logger.Debug("creating HTTP server")
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.APIPort),
		Handler: router,
	}
	cleanupFuncs = append(cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("error shutting down http server", zap.Error(err))
		}
		logger.Info("http server shutted down")
	})

	service := &APIService{}
	service.logger = logger
	service.redisClient = redisClient
	service.server = httpServer
	service.deliveryMQ = deliveryMQ
	service.entityStore = entityStore
	service.eventHandler = eventHandler
	service.deliverymqRetryScheduler = deliverymqRetryScheduler
	service.consumerOptions = &consumerOptions{
		concurreny: cfg.PublishMaxConcurrency,
	}
	service.cleanupFuncs = cleanupFuncs
	if cfg.PublishMQ.GetQueueConfig() != nil {
		service.publishMQ = publishmq.New(publishmq.WithQueue(cfg.PublishMQ.GetQueueConfig()))
	}

	go func() {
		defer wg.Done()
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		service.Shutdown(shutdownCtx)
	}()

	return service, nil
}

func (s *APIService) Run(ctx context.Context) error {
	logger := s.logger.Ctx(ctx)
	logger.Info("service running", zap.String("service", "api"))

	go s.startHTTPServer(ctx)
	go s.startRetrySchedulerMonitor(ctx)
	if s.publishMQ != nil {
		go s.startPublishMQConsumer(ctx)
	}

	return nil
}

func (s *APIService) Shutdown(ctx context.Context) {
	logger := s.logger.Ctx(ctx)
	logger.Info("service shutting down", zap.String("service", "api"))
	for _, cleanupFunc := range s.cleanupFuncs {
		cleanupFunc(ctx, &logger)
	}
	logger.Info("service shutdown", zap.String("service", "api"))
}

func (s *APIService) startHTTPServer(ctx context.Context) {
	logger := s.logger.Ctx(ctx)
	logger.Info("http server listening", zap.String("addr", s.server.Addr))
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("error listening and serving", zap.Error(err))
	}
}

func (s *APIService) startRetrySchedulerMonitor(ctx context.Context) {
	logger := s.logger.Ctx(ctx)
	logger.Info("retry scheduler monitor running")
	if err := s.deliverymqRetryScheduler.Monitor(ctx); err != nil {
		logger.Error("error starting retry scheduler monitor", zap.Error(err))
		return
	}
}

func (s *APIService) startPublishMQConsumer(ctx context.Context) {
	logger := s.logger.Ctx(ctx)
	logger.Info("publishmq consumer running")
	subscription, err := s.publishMQ.Subscribe(ctx)
	if err != nil {
		logger.Error("error subscribing to publishmq", zap.Error(err))
		return
	}
	messageHandler := publishmq.NewMessageHandler(s.eventHandler)
	csm := consumer.New(subscription, messageHandler,
		consumer.WithName("publishmq"),
		consumer.WithConcurrency(s.consumerOptions.concurreny),
	)
	if err := csm.Run(ctx); !errors.Is(err, ctx.Err()) {
		logger.Error("error running publishmq consumer", zap.Error(err))
		return
	}
}
