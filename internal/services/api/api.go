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

	deliveryMQ := deliverymq.New(deliverymq.WithQueue(cfg.DeliveryQueueConfig))
	cleanupDeliveryMQ, err := deliveryMQ.Init(ctx)
	if err != nil {
		return nil, err
	}

	redisClient, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return nil, err
	}

	var logStore models.LogStore
	if cfg.ClickHouse != nil {
		chDB, err := clickhouse.New(cfg.ClickHouse)
		if err != nil {
			return nil, err
		}
		logStore = models.NewLogStore(chDB)
	}

	var eventTracer eventtracer.EventTracer
	if cfg.OpenTelemetry == nil {
		eventTracer = eventtracer.NewNoopEventTracer()
	} else {
		eventTracer = eventtracer.NewEventTracer()
	}
	entityStore := models.NewEntityStore(redisClient, models.NewAESCipher(cfg.EncryptionSecret), cfg.Topics)
	eventHandler := publishmq.NewEventHandler(logger, redisClient, deliveryMQ, entityStore, eventTracer, cfg.Topics)
	router := NewRouter(
		RouterConfig{
			Hostname:       cfg.Hostname,
			APIKey:         cfg.APIKey,
			JWTSecret:      cfg.JWTSecret,
			PortalProxyURL: cfg.PortalProxyURL,
			Topics:         cfg.Topics,
		},
		logger,
		redisClient,
		deliveryMQ,
		entityStore,
		logStore,
		eventHandler,
	)

	// deliverymqRetryScheduler
	deliverymqRetryScheduler := deliverymq.NewRetryScheduler(deliveryMQ, cfg.Redis)
	if err := deliverymqRetryScheduler.Init(ctx); err != nil {
		return nil, err
	}

	service := &APIService{}
	service.logger = logger
	service.redisClient = redisClient
	service.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: router,
	}
	service.publishMQ = publishmq.New(publishmq.WithQueue(cfg.PublishQueueConfig))
	service.deliveryMQ = deliveryMQ
	service.entityStore = entityStore
	service.eventHandler = eventHandler
	service.deliverymqRetryScheduler = deliverymqRetryScheduler
	service.consumerOptions = &consumerOptions{
		concurreny: cfg.PublishMaxConcurrency,
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
			return err
		}
		go func() {
			s.SubscribePublishMQ(ctx, subscription, s.eventHandler, s.consumerOptions.concurreny)
		}()
	}

	return nil
}
