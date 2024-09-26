package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/publishmq"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type consumerOptions struct {
	concurreny int
}

type APIService struct {
	redisClient      *redis.Client
	server           *http.Server
	logger           *otelzap.Logger
	destinationModel *models.DestinationModel
	publishMQ        *publishmq.PublishMQ
	deliveryMQ       *deliverymq.DeliveryMQ
	consumerOptions  *consumerOptions
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

	destinationModel := models.NewDestinationModel(
		models.DestinationModelWithCipher(models.NewAESCipher(cfg.EncryptionSecret)),
	)
	router := NewRouter(
		RouterConfig{
			Hostname:  cfg.Hostname,
			APIKey:    cfg.APIKey,
			JWTSecret: cfg.JWTSecret,
		},
		logger,
		redisClient,
		models.NewTenantModel(),
		destinationModel,
		deliveryMQ,
	)

	service := &APIService{}
	service.logger = logger
	service.redisClient = redisClient
	service.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: router,
	}
	service.destinationModel = destinationModel
	service.publishMQ = publishmq.New(publishmq.WithQueue(cfg.PublishQueueConfig))
	service.deliveryMQ = deliveryMQ
	service.consumerOptions = &consumerOptions{
		concurreny: cfg.PublishMaxConcurrency,
	}

	go func() {
		defer wg.Done()
		<-ctx.Done()
		logger.Ctx(ctx).Info("shutting down", zap.String("service", "api"))
		cleanupDeliveryMQ()
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

	// Subscribe to PublishMQ
	if s.publishMQ != nil {
		subscription, err := s.publishMQ.Subscribe(ctx)
		if err != nil {
			// TODO: handle error
			return err
		}
		go func() {
			s.SubscribePublishMQ(ctx, subscription, s.consumerOptions.concurreny)
		}()
	}

	return nil
}
