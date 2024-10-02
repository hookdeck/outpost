package log

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/hookdeck/EventKit/internal/clickhouse"
	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/logmq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/mikestefanello/batcher"
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

	redisClient, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return nil, err
	}

	chDB, err := clickhouse.New()
	if err != nil {
		return nil, err
	}

	var eventBatcher *batcher.Batcher[*models.Event]
	var deliveryBatcher *batcher.Batcher[*models.Delivery]
	if handler == nil {
		eventBatcher, err = makeEventBatcher(ctx, chDB, models.NewEventModel())
		if err != nil {
			return nil, err
		}
		deliveryBatcher, err = makeDeliveryBatcher(ctx, chDB, models.NewDeliveryModel())
		if err != nil {
			return nil, err
		}

		handler = logmq.NewMessageHandler(logger,
			&handlerEventBatcherImpl{
				batcher: eventBatcher,
			},
			&handlerDeliveryBatcherImpl{
				batcher: deliveryBatcher,
			},
		)
	}

	service := &LogService{}
	service.logger = logger
	service.redisClient = redisClient
	service.logMQ = logmq.New(logmq.WithQueue(cfg.LogQueueConfig))
	service.consumerOptions = &consumerOptions{
		concurreny: cfg.DeliveryMaxConcurrency,
	}
	service.handler = handler

	go func() {
		defer wg.Done()
		<-ctx.Done()
		if eventBatcher != nil {
			eventBatcher.Shutdown()
		}
		if deliveryBatcher != nil {
			deliveryBatcher.Shutdown()
		}
		logger.Ctx(ctx).Info("service shutdown", zap.String("service", "log"))
	}()

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

func makeEventBatcher(ctx context.Context, chDB clickhouse.DB, eventModel *models.EventModel) (*batcher.Batcher[*models.Event], error) {
	b, err := batcher.NewBatcher(batcher.Config[*models.Event]{
		GroupCountThreshold: 2,
		ItemCountThreshold:  100,
		DelayThreshold:      2 * time.Second,
		NumGoroutines:       1,
		Processor: func(_ string, events []*models.Event) {
			// Deduplicate events by event.ID
			uniqueEvents := make([]*models.Event, 0, len(events))
			seen := make(map[string]struct{})
			for _, event := range events {
				if _, exists := seen[event.ID]; !exists {
					seen[event.ID] = struct{}{}
					uniqueEvents = append(uniqueEvents, event)
				}
			}

			err := eventModel.InsertMany(ctx, chDB, uniqueEvents)
			if err != nil {
				// TODO: error handle
				log.Println("eventModel.InsertMany err", err)
			}
		},
	})
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return b, nil
}

type handlerEventBatcherImpl struct {
	batcher *batcher.Batcher[*models.Event]
}

func (b *handlerEventBatcherImpl) Add(ctx context.Context, event *models.Event) error {
	b.batcher.Add("", event)
	return nil
}

func makeDeliveryBatcher(ctx context.Context, chDB clickhouse.DB, deliveryModel *models.DeliveryModel) (*batcher.Batcher[*models.Delivery], error) {
	b, err := batcher.NewBatcher(batcher.Config[*models.Delivery]{
		GroupCountThreshold: 2,
		ItemCountThreshold:  100,
		DelayThreshold:      2 * time.Second,
		NumGoroutines:       1,
		Processor: func(_ string, deliveries []*models.Delivery) {
			// Deduplicate deliveries by event.ID
			uniqueDeliveries := make([]*models.Delivery, 0, len(deliveries))
			seen := make(map[string]struct{})
			for _, delivery := range deliveries {
				if _, exists := seen[delivery.ID]; !exists {
					seen[delivery.ID] = struct{}{}
					uniqueDeliveries = append(uniqueDeliveries, delivery)
				}
			}

			err := deliveryModel.InsertMany(ctx, chDB, uniqueDeliveries)
			if err != nil {
				// TODO: error handle
				log.Println("deliveryModel.InsertMany err", err)
			}
		},
	})
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return b, nil
}

type handlerDeliveryBatcherImpl struct {
	batcher *batcher.Batcher[*models.Delivery]
}

func (b *handlerDeliveryBatcherImpl) Add(ctx context.Context, delivery *models.Delivery) error {
	b.batcher.Add("", delivery)
	return nil
}
