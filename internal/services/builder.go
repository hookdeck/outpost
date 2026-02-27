package services

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/worker"
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

// serviceInstance represents a single service with its cleanup functions and common dependencies
type serviceInstance struct {
	name         string
	cleanupFuncs []func(context.Context, *logging.LoggerWithCtx)

	// Common infrastructure
	redisClient    redis.Client
	logStore       logstore.LogStore
	tenantStore    tenantstore.TenantStore
	destRegistry   destregistry.Registry
	eventTracer    eventtracer.EventTracer
	deliveryMQ     *deliverymq.DeliveryMQ
	logMQ          *logmq.LogMQ
	retryScheduler scheduler.Scheduler

	// HTTP server and router
	router http.Handler
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

// BuildWorkers builds workers based on the configured service type and returns the supervisor.
func (b *ServiceBuilder) BuildWorkers() (*worker.WorkerSupervisor, error) {
	serviceType := b.cfg.MustGetService()
	b.logger.Debug("building workers for service type", zap.String("service_type", serviceType.String()))

	// Create base router with health check that all services will extend
	b.logger.Debug("creating base router with health check")
	baseRouter := NewBaseRouter(b.supervisor, b.cfg.GinMode)

	if serviceType == config.ServiceTypeAPI || serviceType == config.ServiceTypeAll {
		if err := b.BuildAPIWorkers(baseRouter); err != nil {
			b.logger.Error("failed to build API workers", zap.Error(err))
			return nil, err
		}
	}
	if serviceType == config.ServiceTypeDelivery || serviceType == config.ServiceTypeAll {
		if err := b.BuildDeliveryWorker(baseRouter); err != nil {
			b.logger.Error("failed to build delivery worker", zap.Error(err))
			return nil, err
		}
	}
	if serviceType == config.ServiceTypeLog || serviceType == config.ServiceTypeAll {
		if err := b.BuildLogWorker(baseRouter); err != nil {
			b.logger.Error("failed to build log worker", zap.Error(err))
			return nil, err
		}
	}

	// Create HTTP server with the base router
	if err := b.createHTTPServer(baseRouter); err != nil {
		b.logger.Error("failed to create HTTP server", zap.Error(err))
		return nil, err
	}

	return b.supervisor, nil
}

// createHTTPServer creates and registers the HTTP server worker with the given router
func (b *ServiceBuilder) createHTTPServer(router http.Handler) error {
	// Create HTTP server
	b.logger.Debug("creating HTTP server")
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", b.cfg.APIPort),
		Handler: router,
	}

	// Register HTTP server worker
	// Note: HTTP server shutdown is handled by HTTPServerWorker, not in cleanup functions
	httpWorker := NewHTTPServerWorker(httpServer, b.logger)
	b.supervisor.Register(httpWorker)

	b.logger.Info("HTTP server created", zap.String("addr", httpServer.Addr))
	return nil
}

// Cleanup runs all registered cleanup functions for all services.
// Cleanup is performed in LIFO (last-in-first-out) order to ensure that
// resources created later (which may depend on earlier resources) are
// cleaned up before their dependencies.
func (b *ServiceBuilder) Cleanup(ctx context.Context) {
	logger := b.logger.Ctx(ctx)
	// Clean up services in reverse order (LIFO)
	for i := len(b.services) - 1; i >= 0; i-- {
		svc := b.services[i]
		logger.Debug("cleaning up service", zap.String("service", svc.name))
		// Clean up functions within each service in reverse order
		for j := len(svc.cleanupFuncs) - 1; j >= 0; j-- {
			svc.cleanupFuncs[j](ctx, &logger)
		}
	}
}

// BuildAPIWorkers creates the API router and registers workers for the API service.
// This sets up the infrastructure, creates the API router, and registers workers:
// 1. Retry scheduler
// 2. PublishMQ consumer (optional)
// The baseRouter parameter is extended with API routes (apirouter already has health check)
func (b *ServiceBuilder) BuildAPIWorkers(baseRouter *gin.Engine) error {
	b.logger.Debug("building API service workers")

	svc := &serviceInstance{
		name:         "api",
		cleanupFuncs: []func(context.Context, *logging.LoggerWithCtx){},
	}
	b.services = append(b.services, svc)

	// Initialize common infrastructure
	if err := svc.initDestRegistry(b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initDeliveryMQ(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initRedis(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initLogStore(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initEventTracer(b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initTenantStore(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}

	// Initialize retry scheduler
	if err := svc.initRetryScheduler(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}

	// Initialize event handler and create API router
	b.logger.Debug("creating event handler and API router")
	publishIdempotence := idempotence.New(svc.redisClient,
		idempotence.WithTimeout(5*time.Second),
		idempotence.WithSuccessfulTTL(time.Duration(b.cfg.PublishIdempotencyKeyTTL)*time.Second),
		idempotence.WithDeploymentID(b.cfg.DeploymentID),
	)
	eventHandler := publishmq.NewEventHandler(b.logger, svc.deliveryMQ, svc.tenantStore, svc.eventTracer, b.cfg.Topics, publishIdempotence)

	apiHandler := apirouter.NewRouter(
		apirouter.RouterConfig{
			ServiceName:  b.cfg.OpenTelemetry.GetServiceName(),
			APIKey:       b.cfg.APIKey,
			JWTSecret:    b.cfg.APIJWTSecret,
			DeploymentID: b.cfg.DeploymentID,
			Topics:       b.cfg.Topics,
			Registry:     svc.destRegistry,
			PortalConfig: b.cfg.GetPortalConfig(),
			GinMode:      b.cfg.GinMode,
		},
		apirouter.RouterDeps{
			TenantStore:       svc.tenantStore,
			LogStore:          svc.logStore,
			Logger:            b.logger,
			DeliveryPublisher: svc.deliveryMQ,
			EventHandler:      eventHandler,
			Telemetry:         b.telemetry,
		},
	)

	// Mount API handler onto base router (everything except /healthz goes to apiHandler)
	baseRouter.NoRoute(gin.WrapH(apiHandler))

	svc.router = baseRouter

	// Worker 1: RetryMQ Consumer
	retryWorker := NewRetryMQWorker(svc.retryScheduler, b.logger)
	b.supervisor.Register(retryWorker)

	// Worker 2: PublishMQ Consumer (optional)
	if b.cfg.PublishMQ.GetQueueConfig() != nil {
		publishMQ := publishmq.New(publishmq.WithQueue(b.cfg.PublishMQ.GetQueueConfig()))
		messageHandler := publishmq.NewMessageHandler(eventHandler)
		publishMQWorker := NewConsumerWorker(
			"publishmq-consumer",
			publishMQ.Subscribe,
			messageHandler,
			b.cfg.PublishMaxConcurrency,
			b.logger,
		)
		b.supervisor.Register(publishMQWorker)
	}

	b.logger.Info("API service workers built successfully")
	return nil
}

// BuildDeliveryWorker creates and registers the delivery worker.
func (b *ServiceBuilder) BuildDeliveryWorker(baseRouter *gin.Engine) error {
	b.logger.Debug("building delivery service worker")

	svc := &serviceInstance{
		name:         "delivery",
		cleanupFuncs: []func(context.Context, *logging.LoggerWithCtx){},
	}
	b.services = append(b.services, svc)

	// Initialize common infrastructure
	if err := svc.initRedis(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initLogMQ(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initDeliveryMQ(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initDestRegistry(b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initEventTracer(b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initTenantStore(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initLogStore(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}
	if err := svc.initRetryScheduler(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}

	// Initialize alert monitor
	var alertNotifier alert.AlertNotifier
	var destinationDisabler alert.DestinationDisabler
	if b.cfg.Alert.CallbackURL != "" {
		alertNotifier = alert.NewHTTPAlertNotifier(b.cfg.Alert.CallbackURL, alert.NotifierWithBearerToken(b.cfg.APIKey))
	}
	if b.cfg.Alert.AutoDisableDestination {
		destinationDisabler = newDestinationDisabler(svc.tenantStore)
	}
	alertMonitor := alert.NewAlertMonitor(
		b.logger,
		svc.redisClient,
		alert.WithNotifier(alertNotifier),
		alert.WithDisabler(destinationDisabler),
		alert.WithAutoDisableFailureCount(b.cfg.Alert.ConsecutiveFailureCount),
		alert.WithDeploymentID(b.cfg.DeploymentID),
	)

	// Initialize delivery idempotence
	deliveryIdempotence := idempotence.New(svc.redisClient,
		idempotence.WithTimeout(5*time.Second),
		idempotence.WithSuccessfulTTL(time.Duration(b.cfg.DeliveryIdempotencyKeyTTL)*time.Second),
		idempotence.WithDeploymentID(b.cfg.DeploymentID),
	)

	retryBackoff, retryMaxLimit := b.cfg.GetRetryBackoff()

	// Create delivery handler
	handler := deliverymq.NewMessageHandler(
		b.logger,
		svc.logMQ,
		svc.tenantStore,
		svc.destRegistry,
		svc.eventTracer,
		svc.retryScheduler,
		retryBackoff,
		retryMaxLimit,
		alertMonitor,
		deliveryIdempotence,
	)

	svc.router = baseRouter

	// Create DeliveryMQ worker
	deliveryWorker := NewConsumerWorker(
		"deliverymq-consumer",
		svc.deliveryMQ.Subscribe,
		handler,
		b.cfg.DeliveryMaxConcurrency,
		b.logger,
	)
	b.supervisor.Register(deliveryWorker)

	b.logger.Info("delivery service worker built successfully")
	return nil
}

// BuildLogWorker creates and registers the log worker.
func (b *ServiceBuilder) BuildLogWorker(baseRouter *gin.Engine) error {
	b.logger.Debug("building log service worker")

	svc := &serviceInstance{
		name:         "log",
		cleanupFuncs: []func(context.Context, *logging.LoggerWithCtx){},
	}
	b.services = append(b.services, svc)

	// Initialize common infrastructure
	if err := svc.initLogStore(b.ctx, b.cfg, b.logger); err != nil {
		return err
	}

	// Create batcher for batching log writes
	// Convert seconds to duration, treating 0 as "flush immediately" (1ms minimum)
	delayThreshold := time.Duration(b.cfg.LogBatchThresholdSeconds) * time.Second
	if delayThreshold == 0 {
		delayThreshold = time.Millisecond
	}
	batcherCfg := struct {
		ItemCountThreshold int
		DelayThreshold     time.Duration
	}{
		ItemCountThreshold: b.cfg.LogBatchSize,
		DelayThreshold:     delayThreshold,
	}

	b.logger.Debug("creating log batcher")
	batchProcessor, err := logmq.NewBatchProcessor(b.ctx, b.logger, svc.logStore, logmq.BatchProcessorConfig{
		ItemCountThreshold: batcherCfg.ItemCountThreshold,
		DelayThreshold:     batcherCfg.DelayThreshold,
	})
	if err != nil {
		b.logger.Error("failed to create batcher", zap.Error(err))
		return err
	}
	svc.cleanupFuncs = append(svc.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		batchProcessor.Shutdown()
	})

	// Create log handler with batcher
	handler := logmq.NewMessageHandler(b.logger, batchProcessor)

	// Initialize LogMQ
	b.logger.Debug("configuring log message queue")
	logQueueConfig, err := b.cfg.MQs.ToQueueConfig(b.ctx, "logmq")
	if err != nil {
		b.logger.Error("log queue configuration failed", zap.Error(err))
		return err
	}

	logMQ := logmq.New(logmq.WithQueue(logQueueConfig))

	svc.router = baseRouter

	// Create LogMQ worker
	logWorker := NewConsumerWorker(
		"logmq-consumer",
		logMQ.Subscribe,
		handler,
		b.cfg.DeliveryMaxConcurrency,
		b.logger,
	)
	b.supervisor.Register(logWorker)

	b.logger.Info("log service worker built successfully")
	return nil
}

// destinationDisabler implements alert.DestinationDisabler
type destinationDisabler struct {
	tenantStore tenantstore.TenantStore
}

func newDestinationDisabler(tenantStore tenantstore.TenantStore) alert.DestinationDisabler {
	return &destinationDisabler{
		tenantStore: tenantStore,
	}
}

func (d *destinationDisabler) DisableDestination(ctx context.Context, tenantID, destinationID string) (models.Destination, error) {
	destination, err := d.tenantStore.RetrieveDestination(ctx, tenantID, destinationID)
	if err != nil {
		return models.Destination{}, err
	}
	if destination == nil {
		return models.Destination{}, fmt.Errorf("destination not found")
	}
	now := time.Now()
	destination.DisabledAt = &now
	if err := d.tenantStore.UpsertDestination(ctx, *destination); err != nil {
		return models.Destination{}, err
	}
	return *destination, nil
}

// Helper methods for serviceInstance to initialize common dependencies

func (s *serviceInstance) initRedis(ctx context.Context, cfg *config.Config, logger *logging.Logger) error {
	logger.Debug("initializing Redis client", zap.String("service", s.name))
	redisClient, err := redis.New(ctx, cfg.Redis.ToConfig())
	if err != nil {
		logger.Error("Redis client initialization failed", zap.String("service", s.name), zap.Error(err))
		return err
	}
	s.redisClient = redisClient
	return nil
}

func (s *serviceInstance) initLogStore(ctx context.Context, cfg *config.Config, logger *logging.Logger) error {
	logger.Debug("configuring log store driver", zap.String("service", s.name))
	logStoreDriverOpts, err := logstore.MakeDriverOpts(logstore.Config{
		ClickHouse:   cfg.ClickHouse.ToConfig(),
		Postgres:     &cfg.PostgresURL,
		DeploymentID: cfg.DeploymentID,
	})
	if err != nil {
		logger.Error("log store driver configuration failed", zap.String("service", s.name), zap.Error(err))
		return err
	}
	s.cleanupFuncs = append(s.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		logStoreDriverOpts.Close()
	})

	logger.Debug("creating log store", zap.String("service", s.name))
	logStore, err := logstore.NewLogStore(ctx, logStoreDriverOpts)
	if err != nil {
		logger.Error("log store creation failed", zap.String("service", s.name), zap.Error(err))
		return err
	}
	s.logStore = logStore
	return nil
}

func (s *serviceInstance) initTenantStore(ctx context.Context, cfg *config.Config, logger *logging.Logger) error {
	if s.redisClient == nil {
		return fmt.Errorf("redis client must be initialized before tenant store")
	}
	logger.Debug("creating tenant store", zap.String("service", s.name))
	s.tenantStore = tenantstore.New(tenantstore.Config{
		RedisClient:              s.redisClient,
		Secret:                   cfg.AESEncryptionSecret,
		AvailableTopics:          cfg.Topics,
		MaxDestinationsPerTenant: cfg.MaxDestinationsPerTenant,
		DeploymentID:             cfg.DeploymentID,
	})
	if err := s.tenantStore.Init(ctx); err != nil {
		return fmt.Errorf("failed to initialize tenant store: %w", err)
	}
	return nil
}

func (s *serviceInstance) initDestRegistry(cfg *config.Config, logger *logging.Logger) error {
	logger.Debug("initializing destination registry", zap.String("service", s.name))
	registry := destregistry.NewRegistry(&destregistry.Config{
		DestinationMetadataPath: cfg.Destinations.MetadataPath,
		DeliveryTimeout:         time.Duration(cfg.DeliveryTimeoutSeconds) * time.Second,
	}, logger)
	if err := destregistrydefault.RegisterDefault(registry, cfg.Destinations.ToConfig(cfg)); err != nil {
		logger.Error("destination registry setup failed", zap.String("service", s.name), zap.Error(err))
		return err
	}
	s.destRegistry = registry
	return nil
}

func (s *serviceInstance) initEventTracer(cfg *config.Config, logger *logging.Logger) error {
	logger.Debug("setting up event tracer", zap.String("service", s.name))
	if cfg.OpenTelemetry.ToConfig() == nil {
		s.eventTracer = eventtracer.NewNoopEventTracer()
	} else {
		s.eventTracer = eventtracer.NewEventTracer()
	}
	return nil
}

func (s *serviceInstance) initDeliveryMQ(ctx context.Context, cfg *config.Config, logger *logging.Logger) error {
	logger.Debug("configuring delivery message queue", zap.String("service", s.name))
	deliveryQueueConfig, err := cfg.MQs.ToQueueConfig(ctx, "deliverymq")
	if err != nil {
		logger.Error("delivery queue configuration failed", zap.String("service", s.name), zap.Error(err))
		return err
	}

	logger.Debug("initializing delivery MQ connection", zap.String("service", s.name), zap.String("mq_type", cfg.MQs.GetInfraType()))
	deliveryMQ := deliverymq.New(deliverymq.WithQueue(deliveryQueueConfig))
	cleanupDeliveryMQ, err := deliveryMQ.Init(ctx)
	if err != nil {
		logger.Error("delivery MQ initialization failed", zap.String("service", s.name), zap.Error(err))
		return err
	}
	s.cleanupFuncs = append(s.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) { cleanupDeliveryMQ() })
	s.deliveryMQ = deliveryMQ
	return nil
}

func (s *serviceInstance) initLogMQ(ctx context.Context, cfg *config.Config, logger *logging.Logger) error {
	logger.Debug("configuring log message queue", zap.String("service", s.name))
	logQueueConfig, err := cfg.MQs.ToQueueConfig(ctx, "logmq")
	if err != nil {
		logger.Error("log queue configuration failed", zap.String("service", s.name), zap.Error(err))
		return err
	}

	logger.Debug("initializing log MQ connection", zap.String("service", s.name), zap.String("mq_type", cfg.MQs.GetInfraType()))
	logMQ := logmq.New(logmq.WithQueue(logQueueConfig))
	cleanupLogMQ, err := logMQ.Init(ctx)
	if err != nil {
		logger.Error("log MQ initialization failed", zap.String("service", s.name), zap.Error(err))
		return err
	}
	s.cleanupFuncs = append(s.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) { cleanupLogMQ() })
	s.logMQ = logMQ
	return nil
}

func (s *serviceInstance) initRetryScheduler(ctx context.Context, cfg *config.Config, logger *logging.Logger) error {
	if s.deliveryMQ == nil {
		return fmt.Errorf("delivery MQ must be initialized before retry scheduler")
	}
	if s.logStore == nil {
		return fmt.Errorf("log store must be initialized before retry scheduler")
	}
	logger.Debug("creating delivery MQ retry scheduler", zap.String("service", s.name))
	pollBackoff := time.Duration(cfg.RetryPollBackoffMs) * time.Millisecond
	var retrySchedulerOpts []deliverymq.RetrySchedulerOption
	if cfg.RetryVisibilityTimeoutSeconds > 0 {
		retrySchedulerOpts = append(retrySchedulerOpts, deliverymq.WithRetryVisibilityTimeout(uint(cfg.RetryVisibilityTimeoutSeconds)))
	}
	retryScheduler, err := deliverymq.NewRetryScheduler(s.deliveryMQ, cfg.Redis.ToConfig(), cfg.DeploymentID, pollBackoff, logger, s.logStore, retrySchedulerOpts...)
	if err != nil {
		logger.Error("failed to create delivery MQ retry scheduler", zap.String("service", s.name), zap.Error(err))
		return err
	}
	logger.Debug("initializing delivery MQ retry scheduler", zap.String("service", s.name))
	if err := retryScheduler.Init(ctx); err != nil {
		logger.Error("delivery MQ retry scheduler initialization failed", zap.String("service", s.name), zap.Error(err))
		return err
	}
	s.cleanupFuncs = append(s.cleanupFuncs, func(ctx context.Context, logger *logging.LoggerWithCtx) {
		retryScheduler.Shutdown()
	})
	s.retryScheduler = retryScheduler
	return nil
}
