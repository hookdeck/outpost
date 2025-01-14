package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type consumerOptions struct {
	concurreny int
}

type APIService struct {
	registry                 destregistry.Registry
	redisClient              *redis.Client
	server                   *http.Server
	logger                   *otelzap.Logger
	publishMQ                *publishmq.PublishMQ
	deliveryMQ               *deliverymq.DeliveryMQ
	entityStore              models.EntityStore
	eventHandler             publishmq.EventHandler
	deliverymqRetryScheduler scheduler.Scheduler
	consumerOptions          *consumerOptions
}

func NewService(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *otelzap.Logger) (*APIService, error) {
	wg.Add(1)

	registry := destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: cfg.Destinations.MetadataPath,
		DeliveryTimeout:         time.Duration(cfg.DeliveryTimeoutSeconds) * time.Second,
	}, logger)
	if err := destregistrydefault.RegisterDefault(registry, cfg.Destinations.ToConfig()); err != nil {
		return nil, err
	}

	deliveryMQ := deliverymq.New(deliverymq.WithQueue(cfg.MQs.GetDeliveryQueueConfig()))
	cleanupDeliveryMQ, err := deliveryMQ.Init(ctx)
	if err != nil {
		return nil, err
	}

	redisClient, err := redis.New(ctx, cfg.Redis.ToConfig())
	if err != nil {
		return nil, err
	}

	chDB, err := clickhouse.New(cfg.ClickHouse.ToConfig())
	if err != nil {
		return nil, err
	}
	logStore := models.NewLogStore(chDB)

	var eventTracer eventtracer.EventTracer
	if cfg.OpenTelemetry.ToConfig() == nil {
		eventTracer = eventtracer.NewNoopEventTracer()
	} else {
		eventTracer = eventtracer.NewEventTracer()
	}
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher(cfg.AESEncryptionSecret)),
		models.WithAvailableTopics(cfg.Topics),
		models.WithMaxDestinationsPerTenant(cfg.MaxDestinationsPerTenant),
	)
	eventHandler := publishmq.NewEventHandler(logger, redisClient, deliveryMQ, entityStore, eventTracer, cfg.Topics)
	router := NewRouter(
		RouterConfig{
			ServiceName:  cfg.OpenTelemetry.GetServiceName(),
			APIKey:       cfg.APIKey,
			JWTSecret:    cfg.APIJWTSecret,
			Topics:       cfg.Topics,
			Registry:     registry,
			PortalConfig: cfg.GetPortalConfig(),
		},
		logger,
		redisClient,
		deliveryMQ,
		entityStore,
		logStore,
		eventHandler,
	)

	// deliverymqRetryScheduler
	deliverymqRetryScheduler := deliverymq.NewRetryScheduler(deliveryMQ, cfg.Redis.ToConfig())
	if err := deliverymqRetryScheduler.Init(ctx); err != nil {
		return nil, err
	}

	service := &APIService{}
	service.logger = logger
	service.redisClient = redisClient
	service.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.APIPort),
		Handler: router,
	}
	service.deliveryMQ = deliveryMQ
	service.entityStore = entityStore
	service.eventHandler = eventHandler
	service.deliverymqRetryScheduler = deliverymqRetryScheduler
	service.consumerOptions = &consumerOptions{
		concurreny: cfg.PublishMaxConcurrency,
	}
	if cfg.PublishMQ.GetQueueConfig() != nil {
		service.publishMQ = publishmq.New(publishmq.WithQueue(cfg.PublishMQ.GetQueueConfig()))
	}

	go func() {
		defer wg.Done()
		<-ctx.Done()
		logger.Ctx(ctx).Info("shutting down", zap.String("service", "api"))
		cleanupDeliveryMQ()
		deliverymqRetryScheduler.Shutdown()
		// make a new context for Shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := service.server.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "error shutting down http server: %s\n", err)
		}
		logger.Ctx(ctx).Info("http server shutted down")
		logger.Ctx(ctx).Info("service shutdown", zap.String("service", "api"))
	}()

	return service, nil
}

func (s *APIService) Run(ctx context.Context) error {
	s.logger.Ctx(ctx).Info("start service", zap.String("service", "api"))

	// Start HTTP server
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Ctx(ctx).Error(fmt.Sprintf("error listening and serving: %s\n", err), zap.Error(err))
		}
	}()

	// Monitor deliverymq retry scheduler
	go s.deliverymqRetryScheduler.Monitor(ctx)

	// Subscribe to PublishMQ
	if s.publishMQ != nil {
		subscription, err := s.publishMQ.Subscribe(ctx)
		if err != nil {
			// TODO: handle error
			s.logger.Ctx(ctx).Error("failed to subscribe to publishmq", zap.Error(err))
			return err
		}
		go func() {
			s.SubscribePublishMQ(ctx, subscription, s.eventHandler, s.consumerOptions.concurreny)
		}()
	}

	return nil
}
