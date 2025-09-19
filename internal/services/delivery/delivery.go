package delivery

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"go.uber.org/zap"
	_ "gocloud.dev/pubsub/mempubsub"
)

type consumerOptions struct {
	concurreny int
}

type DeliveryService struct {
	cleanupFuncs []func()

	consumerOptions *consumerOptions
	Logger          *logging.Logger
	RedisClient     redis.Cmdable
	DeliveryMQ      *deliverymq.DeliveryMQ
	Handler         consumer.MessageHandler
}

func NewService(ctx context.Context,
	wg *sync.WaitGroup,
	cfg *config.Config,
	logger *logging.Logger,
	handler consumer.MessageHandler,
) (*DeliveryService, error) {
	wg.Add(1)

	cleanupFuncs := []func(){}

	// Create Redis client for all operations (now cluster-compatible)
	redisClient, err := redis.New(ctx, cfg.Redis.ToConfig())
	if err != nil {
		return nil, err
	}

	logmqConfig, err := cfg.MQs.ToQueueConfig(ctx, "logmq")
	if err != nil {
		return nil, err
	}
	deliverymqConfig, err := cfg.MQs.ToQueueConfig(ctx, "deliverymq")
	if err != nil {
		return nil, err
	}

	logMQ := logmq.New(logmq.WithQueue(logmqConfig))
	cleanupLogMQ, err := logMQ.Init(ctx)
	if err != nil {
		return nil, err
	}
	cleanupFuncs = append(cleanupFuncs, cleanupLogMQ)

	deliveryMQ := deliverymq.New(deliverymq.WithQueue(deliverymqConfig))
	cleanupDeliveryMQ, err := deliveryMQ.Init(ctx)
	if err != nil {
		return nil, err
	}
	cleanupFuncs = append(cleanupFuncs, cleanupDeliveryMQ)

	if handler == nil {
		registry := destregistry.NewRegistry(&destregistry.Config{
			DestinationMetadataPath: cfg.Destinations.MetadataPath,
			DeliveryTimeout:         time.Duration(cfg.DeliveryTimeoutSeconds) * time.Second,
		}, logger)
		if err := destregistrydefault.RegisterDefault(registry, cfg.Destinations.ToConfig(cfg)); err != nil {
			return nil, err
		}
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

		logstoreDriverOpts, err := logstore.MakeDriverOpts(logstore.Config{
			// ClickHouse: cfg.ClickHouse.ToConfig(),
			Postgres: &cfg.PostgresURL,
		})
		if err != nil {
			return nil, err
		}
		cleanupFuncs = append(cleanupFuncs, func() {
			logstoreDriverOpts.Close()
		})
		logStore, err := logstore.NewLogStore(ctx, logstoreDriverOpts)
		if err != nil {
			return nil, err
		}

		retryScheduler := deliverymq.NewRetryScheduler(deliveryMQ, cfg.Redis.ToConfig(), logger)
		if err := retryScheduler.Init(ctx); err != nil {
			return nil, err
		}
		cleanupFuncs = append(cleanupFuncs, func() {
			retryScheduler.Shutdown()
		})

		var alertNotifier alert.AlertNotifier
		var destinationDisabler alert.DestinationDisabler
		if cfg.Alert.CallbackURL != "" {
			alertNotifier = alert.NewHTTPAlertNotifier(cfg.Alert.CallbackURL, alert.NotifierWithBearerToken(cfg.APIKey))
		}
		if cfg.Alert.AutoDisableDestination {
			destinationDisabler = newDestinationDisabler(entityStore)
		}
		alertMonitor := alert.NewAlertMonitor(
			logger,
			redisClient,
			alert.WithNotifier(alertNotifier),
			alert.WithDisabler(destinationDisabler),
			alert.WithAutoDisableFailureCount(cfg.Alert.ConsecutiveFailureCount),
		)

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
			cfg.RetryMaxLimit,
			alertMonitor,
		)
	}

	service := &DeliveryService{
		Logger:      logger,
		RedisClient: redisClient,
		Handler:     handler,
		DeliveryMQ:  deliveryMQ,
		consumerOptions: &consumerOptions{
			concurreny: cfg.DeliveryMaxConcurrency,
		},
		cleanupFuncs: cleanupFuncs,
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

func (s *DeliveryService) Run(ctx context.Context) error {
	s.Logger.Ctx(ctx).Info("service running", zap.String("service", "delivery"))

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

func (s *DeliveryService) Shutdown(ctx context.Context) {
	logger := s.Logger.Ctx(ctx)
	logger.Info("service shutting down", zap.String("service", "delivery"))
	for _, cleanup := range s.cleanupFuncs {
		cleanup()
	}
	logger.Info("service shutdown", zap.String("service", "delivery"))
}

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
