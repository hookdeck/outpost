package services

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	apirouter "github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/worker"
	"github.com/mikestefanello/batcher"
	"go.uber.org/zap"
)

// ServiceBuilder constructs workers based on service configuration.
type ServiceBuilder struct {
	ctx        context.Context
	cfg        *config.Config
	logger     *logging.Logger
	telemetry  telemetry.Telemetry
	supervisor *worker.WorkerSupervisor

	// Track service instances for cleanup
	services []*serviceInstance
}

// serviceInstance represents a single service with its cleanup functions
type serviceInstance struct {
	name         string
	cleanupFuncs []func(context.Context, *logging.LoggerWithCtx)
}

// NewServiceBuilder creates a new ServiceBuilder.
func NewServiceBuilder(ctx context.Context, cfg *config.Config, logger *logging.Logger, telemetry telemetry.Telemetry) *ServiceBuilder {
	return &ServiceBuilder{
		ctx:        ctx,
		cfg:        cfg,
		logger:     logger,
		telemetry:  telemetry,
		supervisor: worker.NewWorkerSupervisor(logger),
		services:   []*serviceInstance{},
	}
}

// BuildAPIWorkers creates and registers all workers for the API service.
// This sets up the infrastructure and creates 3 workers:
// 1. HTTP server
// 2. Retry scheduler
// 3. PublishMQ consumer (optional)
func (b *ServiceBuilder) BuildAPIWorkers() error {
	b.logger.Debug("building API service workers")

	// Create a new service instance for API
	svc := &serviceInstance{
		name:         "api",
		cleanupFuncs: []func(context.Context, *logging.LoggerWithCtx){},
	}
	b.services = append(b.services, svc)

	// Initialize destination registry
	b.logger.Debug("initializing destination registry")
	registry := destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: b.cfg.Destinations.MetadataPath,
		DeliveryTimeout:         time.Duration(b.cfg.DeliveryTimeoutSeconds) * time.Second,
	}, b.logger)
	if err := destregistrydefault.RegisterDefault(registry, b.cfg.Destinations.ToConfig(b.cfg)); err != nil {
		b.logger.Error("destination registry setup failed", zap.Error(err))
		return err
	}

	// Initialize delivery MQ
	b.logger.Debug("configuring delivery message queue")
	deliveryQueueConfig, err := b.cfg.MQs.ToQueueConfig(b.ctx, "deliverymq")
	if err != nil {
		b.logger.Error("delivery queue configuration failed", zap.Error(err))
		return err
	}

	b.logger.Debug("initializing delivery MQ connection", zap.String("mq_type", b.cfg.MQs.GetInfraType()))
	deliveryMQ := deliverymq.New(deliverymq.WithQueue(deliveryQueueConfig))
	cleanupDeliveryMQ, err := deliveryMQ.Init(b.ctx)
	if err != nil {
		b.logger.Error("delivery MQ initialization failed", zap.Error(err))
		return err
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) { cleanupDeliveryMQ() })

	// Initialize Redis
	b.logger.Debug("initializing Redis client for API service")
	redisClient, err := redis.New(b.ctx, b.cfg.Redis.ToConfig())
	if err != nil {
		b.logger.Error("Redis client initialization failed in API service", zap.Error(err))
		return err
	}

	// Initialize log store
	b.logger.Debug("configuring log store driver")
	logStoreDriverOpts, err := logstore.MakeDriverOpts(logstore.Config{
		Postgres: &b.cfg.PostgresURL,
	})
	if err != nil {
		b.logger.Error("log store driver configuration failed", zap.Error(err))
		return err
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		logStoreDriverOpts.Close()
	})

	b.logger.Debug("creating log store")
	logStore, err := logstore.NewLogStore(b.ctx, logStoreDriverOpts)
	if err != nil {
		b.logger.Error("log store creation failed", zap.Error(err))
		return err
	}

	// Initialize event tracer
	b.logger.Debug("setting up event tracer")
	var eventTracer eventtracer.EventTracer
	if b.cfg.OpenTelemetry.ToConfig() == nil {
		eventTracer = eventtracer.NewNoopEventTracer()
	} else {
		eventTracer = eventtracer.NewEventTracer()
	}

	// Initialize entity store
	b.logger.Debug("creating entity store with Redis client")
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher(b.cfg.AESEncryptionSecret)),
		models.WithAvailableTopics(b.cfg.Topics),
		models.WithMaxDestinationsPerTenant(b.cfg.MaxDestinationsPerTenant),
		models.WithDeploymentID(b.cfg.DeploymentID),
	)

	// Initialize event handler and router
	b.logger.Debug("creating event handler and router")
	publishIdempotence := idempotence.New(redisClient,
		idempotence.WithTimeout(5*time.Second),
		idempotence.WithSuccessfulTTL(time.Duration(b.cfg.PublishIdempotencyKeyTTL)*time.Second),
	)
	eventHandler := publishmq.NewEventHandler(b.logger, deliveryMQ, entityStore, eventTracer, b.cfg.Topics, publishIdempotence)
	router := apirouter.NewRouter(
		apirouter.RouterConfig{
			ServiceName:  b.cfg.OpenTelemetry.GetServiceName(),
			APIKey:       b.cfg.APIKey,
			JWTSecret:    b.cfg.APIJWTSecret,
			Topics:       b.cfg.Topics,
			Registry:     registry,
			PortalConfig: b.cfg.GetPortalConfig(),
			GinMode:      b.cfg.GinMode,
		},
		b.logger,
		redisClient,
		deliveryMQ,
		entityStore,
		logStore,
		eventHandler,
		b.telemetry,
	)

	// Initialize retry scheduler
	b.logger.Debug("creating delivery MQ retry scheduler")
	deliverymqRetryScheduler, err := deliverymq.NewRetryScheduler(deliveryMQ, b.cfg.Redis.ToConfig(), b.cfg.DeploymentID, b.logger)
	if err != nil {
		b.logger.Error("failed to create delivery MQ retry scheduler", zap.Error(err))
		return err
	}
	b.logger.Debug("initializing delivery MQ retry scheduler")
	if err := deliverymqRetryScheduler.Init(b.ctx); err != nil {
		b.logger.Error("delivery MQ retry scheduler initialization failed", zap.Error(err))
		return err
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		deliverymqRetryScheduler.Shutdown()
	})

	// Create HTTP server
	b.logger.Debug("creating HTTP server")
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", b.cfg.APIPort),
		Handler: router,
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("error shutting down http server", zap.Error(err))
		}
		logger.Info("http server shut down")
	})

	// Worker 1: HTTP Server
	httpWorker := NewHTTPServerWorker(httpServer, b.logger)
	b.supervisor.Register(httpWorker)

	// Worker 2: Retry Scheduler
	retryWorker := NewRetrySchedulerWorker(deliverymqRetryScheduler, b.logger)
	b.supervisor.Register(retryWorker)

	// Worker 3: PublishMQ Consumer (optional)
	if b.cfg.PublishMQ.GetQueueConfig() != nil {
		publishMQ := publishmq.New(publishmq.WithQueue(b.cfg.PublishMQ.GetQueueConfig()))
		publishMQWorker := NewPublishMQWorker(
			publishMQ,
			eventHandler,
			b.cfg.PublishMaxConcurrency,
			b.logger,
		)
		b.supervisor.Register(publishMQWorker)
	}

	b.logger.Info("API service workers built successfully")
	return nil
}

// BuildDeliveryWorker creates and registers the delivery worker.
func (b *ServiceBuilder) BuildDeliveryWorker() error {
	b.logger.Debug("building delivery service worker")

	// Create a new service instance for Delivery
	svc := &serviceInstance{
		name:         "delivery",
		cleanupFuncs: []func(context.Context, *logging.LoggerWithCtx){},
	}
	b.services = append(b.services, svc)

	// Initialize Redis
	b.logger.Debug("initializing Redis client for delivery service")
	redisClient, err := redis.New(b.ctx, b.cfg.Redis.ToConfig())
	if err != nil {
		b.logger.Error("Redis client initialization failed in delivery service", zap.Error(err))
		return err
	}

	// Initialize LogMQ
	b.logger.Debug("configuring log message queue")
	logQueueConfig, err := b.cfg.MQs.ToQueueConfig(b.ctx, "logmq")
	if err != nil {
		b.logger.Error("log queue configuration failed", zap.Error(err))
		return err
	}

	b.logger.Debug("initializing log MQ connection", zap.String("mq_type", b.cfg.MQs.GetInfraType()))
	logMQ := logmq.New(logmq.WithQueue(logQueueConfig))
	cleanupLogMQ, err := logMQ.Init(b.ctx)
	if err != nil {
		b.logger.Error("log MQ initialization failed", zap.Error(err))
		return err
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) { cleanupLogMQ() })

	// Initialize DeliveryMQ
	b.logger.Debug("configuring delivery message queue")
	deliveryQueueConfig, err := b.cfg.MQs.ToQueueConfig(b.ctx, "deliverymq")
	if err != nil {
		b.logger.Error("delivery queue configuration failed", zap.Error(err))
		return err
	}

	b.logger.Debug("initializing delivery MQ connection", zap.String("mq_type", b.cfg.MQs.GetInfraType()))
	deliveryMQ := deliverymq.New(deliverymq.WithQueue(deliveryQueueConfig))
	cleanupDeliveryMQ, err := deliveryMQ.Init(b.ctx)
	if err != nil {
		b.logger.Error("delivery MQ initialization failed", zap.Error(err))
		return err
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) { cleanupDeliveryMQ() })

	// Initialize destination registry
	b.logger.Debug("initializing destination registry")
	registry := destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: b.cfg.Destinations.MetadataPath,
		DeliveryTimeout:         time.Duration(b.cfg.DeliveryTimeoutSeconds) * time.Second,
	}, b.logger)
	if err := destregistrydefault.RegisterDefault(registry, b.cfg.Destinations.ToConfig(b.cfg)); err != nil {
		b.logger.Error("destination registry setup failed", zap.Error(err))
		return err
	}

	// Initialize event tracer
	b.logger.Debug("setting up event tracer")
	var eventTracer eventtracer.EventTracer
	if b.cfg.OpenTelemetry.ToConfig() == nil {
		eventTracer = eventtracer.NewNoopEventTracer()
	} else {
		eventTracer = eventtracer.NewEventTracer()
	}

	// Initialize entity store
	b.logger.Debug("creating entity store with Redis client")
	entityStore := models.NewEntityStore(redisClient,
		models.WithCipher(models.NewAESCipher(b.cfg.AESEncryptionSecret)),
		models.WithAvailableTopics(b.cfg.Topics),
		models.WithMaxDestinationsPerTenant(b.cfg.MaxDestinationsPerTenant),
		models.WithDeploymentID(b.cfg.DeploymentID),
	)

	// Initialize log store
	b.logger.Debug("configuring log store driver")
	logStoreDriverOpts, err := logstore.MakeDriverOpts(logstore.Config{
		Postgres: &b.cfg.PostgresURL,
	})
	if err != nil {
		b.logger.Error("log store driver configuration failed", zap.Error(err))
		return err
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		logStoreDriverOpts.Close()
	})

	b.logger.Debug("creating log store")
	logStore, err := logstore.NewLogStore(b.ctx, logStoreDriverOpts)
	if err != nil {
		b.logger.Error("log store creation failed", zap.Error(err))
		return err
	}

	// Initialize retry scheduler
	b.logger.Debug("creating delivery MQ retry scheduler")
	retryScheduler, err := deliverymq.NewRetryScheduler(deliveryMQ, b.cfg.Redis.ToConfig(), b.cfg.DeploymentID, b.logger)
	if err != nil {
		b.logger.Error("failed to create delivery MQ retry scheduler", zap.Error(err))
		return err
	}
	b.logger.Debug("initializing delivery MQ retry scheduler")
	if err := retryScheduler.Init(b.ctx); err != nil {
		b.logger.Error("delivery MQ retry scheduler initialization failed", zap.Error(err))
		return err
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		retryScheduler.Shutdown()
	})

	// Initialize alert monitor
	var alertNotifier alert.AlertNotifier
	var destinationDisabler alert.DestinationDisabler
	if b.cfg.Alert.CallbackURL != "" {
		alertNotifier = alert.NewHTTPAlertNotifier(b.cfg.Alert.CallbackURL, alert.NotifierWithBearerToken(b.cfg.APIKey))
	}
	if b.cfg.Alert.AutoDisableDestination {
		destinationDisabler = newDestinationDisabler(entityStore)
	}
	alertMonitor := alert.NewAlertMonitor(
		b.logger,
		redisClient,
		alert.WithNotifier(alertNotifier),
		alert.WithDisabler(destinationDisabler),
		alert.WithAutoDisableFailureCount(b.cfg.Alert.ConsecutiveFailureCount),
		alert.WithDeploymentID(b.cfg.DeploymentID),
	)

	// Initialize delivery idempotence
	deliveryIdempotence := idempotence.New(redisClient,
		idempotence.WithTimeout(5*time.Second),
		idempotence.WithSuccessfulTTL(time.Duration(b.cfg.DeliveryIdempotencyKeyTTL)*time.Second),
	)

	// Get retry configuration
	retryBackoff, retryMaxLimit := b.cfg.GetRetryBackoff()

	// Create delivery handler
	handler := deliverymq.NewMessageHandler(
		b.logger,
		logMQ,
		entityStore,
		logStore,
		registry,
		eventTracer,
		retryScheduler,
		retryBackoff,
		retryMaxLimit,
		alertMonitor,
		deliveryIdempotence,
	)

	// Create DeliveryMQ worker
	deliveryWorker := NewDeliveryMQWorker(
		deliveryMQ,
		handler,
		b.cfg.DeliveryMaxConcurrency,
		b.logger,
	)
	b.supervisor.Register(deliveryWorker)

	b.logger.Info("delivery service worker built successfully")
	return nil
}

// BuildLogWorker creates and registers the log worker.
func (b *ServiceBuilder) BuildLogWorker() error {
	b.logger.Debug("building log service worker")

	// Create a new service instance for Log
	svc := &serviceInstance{
		name:         "log",
		cleanupFuncs: []func(context.Context, *logging.LoggerWithCtx){},
	}
	b.services = append(b.services, svc)

	// Initialize log store
	b.logger.Debug("configuring log store driver")
	logStoreDriverOpts, err := logstore.MakeDriverOpts(logstore.Config{
		Postgres: &b.cfg.PostgresURL,
	})
	if err != nil {
		b.logger.Error("log store driver configuration failed", zap.Error(err))
		return err
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		logStoreDriverOpts.Close()
	})

	b.logger.Debug("creating log store")
	logStore, err := logstore.NewLogStore(b.ctx, logStoreDriverOpts)
	if err != nil {
		b.logger.Error("log store creation failed", zap.Error(err))
		return err
	}

	// Create batcher for batching log writes
	batcherCfg := struct {
		ItemCountThreshold int
		DelayThreshold     time.Duration
	}{
		ItemCountThreshold: b.cfg.LogBatchSize,
		DelayThreshold:     time.Duration(b.cfg.LogBatchThresholdSeconds) * time.Second,
	}

	b.logger.Debug("creating log batcher")
	batcher, err := b.makeBatcher(logStore, batcherCfg.ItemCountThreshold, batcherCfg.DelayThreshold)
	if err != nil {
		b.logger.Error("failed to create batcher", zap.Error(err))
		return err
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		batcher.Shutdown()
	})

	// Create log handler with batcher
	handler := logmq.NewMessageHandler(b.logger, &handlerBatcherImpl{batcher: batcher})

	// Initialize LogMQ
	b.logger.Debug("configuring log message queue")
	logQueueConfig, err := b.cfg.MQs.ToQueueConfig(b.ctx, "logmq")
	if err != nil {
		b.logger.Error("log queue configuration failed", zap.Error(err))
		return err
	}

	logMQ := logmq.New(logmq.WithQueue(logQueueConfig))

	// Create LogMQ worker
	logWorker := NewLogMQWorker(
		logMQ,
		handler,
		b.cfg.DeliveryMaxConcurrency,
		b.logger,
	)
	b.supervisor.Register(logWorker)

	b.logger.Info("log service worker built successfully")
	return nil
}

// BuildWorkers builds workers based on the configured service type.
func (b *ServiceBuilder) BuildWorkers() error {
	serviceType := b.cfg.MustGetService()
	b.logger.Debug("building workers for service type", zap.String("service_type", serviceType.String()))

	if serviceType == config.ServiceTypeAPI || serviceType == config.ServiceTypeAll {
		if err := b.BuildAPIWorkers(); err != nil {
			b.logger.Error("failed to build API workers", zap.Error(err))
			return err
		}
	}
	if serviceType == config.ServiceTypeDelivery || serviceType == config.ServiceTypeAll {
		if err := b.BuildDeliveryWorker(); err != nil {
			b.logger.Error("failed to build delivery worker", zap.Error(err))
			return err
		}
	}
	if serviceType == config.ServiceTypeLog || serviceType == config.ServiceTypeAll {
		if err := b.BuildLogWorker(); err != nil {
			b.logger.Error("failed to build log worker", zap.Error(err))
			return err
		}
	}

	return nil
}

// Build returns the configured WorkerSupervisor.
func (b *ServiceBuilder) Build() (*worker.WorkerSupervisor, error) {
	return b.supervisor, nil
}

// Cleanup runs all registered cleanup functions for all services.
func (b *ServiceBuilder) Cleanup(ctx context.Context) {
	logger := b.logger.Ctx(ctx)
	for _, svc := range b.services {
		logger.Debug("cleaning up service", zap.String("service", svc.name))
		for _, cleanupFunc := range svc.cleanupFuncs {
			cleanupFunc(ctx, &logger)
		}
	}
}

// destinationDisabler implements alert.DestinationDisabler
type destinationDisabler struct {
	entityStore models.EntityStore
}

func newDestinationDisabler(entityStore models.EntityStore) alert.DestinationDisabler {
	return &destinationDisabler{
		entityStore: entityStore,
	}
}

func (d *destinationDisabler) DisableDestination(ctx context.Context, tenantID, destinationID string) error {
	destination, err := d.entityStore.RetrieveDestination(ctx, tenantID, destinationID)
	if err != nil {
		return err
	}
	if destination == nil {
		return nil
	}
	now := time.Now()
	destination.DisabledAt = &now
	return d.entityStore.UpsertDestination(ctx, *destination)
}

// makeBatcher creates a batcher for batching log writes
func (b *ServiceBuilder) makeBatcher(logStore logstore.LogStore, itemCountThreshold int, delayThreshold time.Duration) (*batcher.Batcher[*mqs.Message], error) {
	batchr, err := batcher.NewBatcher(batcher.Config[*mqs.Message]{
		GroupCountThreshold: 2,
		ItemCountThreshold:  itemCountThreshold,
		DelayThreshold:      delayThreshold,
		NumGoroutines:       1,
		Processor: func(_ string, msgs []*mqs.Message) {
			logger := b.logger.Ctx(b.ctx)
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
					logger.Error("failed to parse delivery event",
						zap.Error(err),
						zap.String("message_id", msg.LoggableID))
					nackAll()
					return
				}
				deliveryEvents = append(deliveryEvents, &deliveryEvent)
			}

			if err := logStore.InsertManyDeliveryEvent(b.ctx, deliveryEvents); err != nil {
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
		b.logger.Ctx(b.ctx).Error("failed to create batcher", zap.Error(err))
		return nil, err
	}
	return batchr, nil
}

// handlerBatcherImpl implements the batcher interface expected by logmq.MessageHandler
type handlerBatcherImpl struct {
	batcher *batcher.Batcher[*mqs.Message]
}

func (hb *handlerBatcherImpl) Add(ctx context.Context, msg *mqs.Message) error {
	hb.batcher.Add("", msg)
	return nil
}
