package log

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/redis"
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
	if cfg.ClickHouse == nil {
		return nil, errors.New("missing clickhouse configuration")
	}

	wg.Add(1)

	redisClient, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return nil, err
	}

	chDB, err := clickhouse.New(cfg.ClickHouse)
	if err != nil {
		return nil, err
	}

	var eventBatcher *batcher.Batcher[*models.Event]
	var deliveryBatcher *batcher.Batcher[*models.Delivery]
	if handler == nil {
		batcher, err := makeBatcher(ctx, logger, models.NewLogStore(chDB))
		if err != nil {
			return nil, err
		}

		handler = logmq.NewMessageHandler(logger, &handlerBatcherImpl{batcher: batcher})
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

func makeBatcher(ctx context.Context, logger *otelzap.Logger, logStore models.LogStore) (*batcher.Batcher[*mqs.Message], error) {
	b, err := batcher.NewBatcher(batcher.Config[*mqs.Message]{
		GroupCountThreshold: 2,
		ItemCountThreshold:  100,
		DelayThreshold:      5 * time.Second,
		NumGoroutines:       1,
		Processor: func(_ string, msgs []*mqs.Message) {
			logger.Ctx(ctx).Info("log batcher processor", zap.Int("msgs", len(msgs)))

			nackAll := func() {
				for _, msg := range msgs {
					msg.Nack()
				}
			}

			deliveryEvents := make([]*models.DeliveryEvent, 0, len(msgs))
			for _, msg := range msgs {
				deliveryEvent := models.DeliveryEvent{}
				if err := deliveryEvent.FromMessage(msg); err != nil {
					// TODO: handle error
					log.Println("deliveryEvent.FromMessage err", err)
					nackAll() // TODO: handle individual nack
					return
				}
				deliveryEvents = append(deliveryEvents, &deliveryEvent)
			}

			// Deduplicate events by event.ID
			uniqueEvents := make([]*models.Event, 0, len(deliveryEvents))
			seenEvents := make(map[string]struct{})
			for _, deliveryEvent := range deliveryEvents {
				event := deliveryEvent.Event
				if _, exists := seenEvents[event.ID]; !exists {
					seenEvents[event.ID] = struct{}{}
					uniqueEvents = append(uniqueEvents, &event)
				}
			}

			err := logStore.InsertManyEvent(ctx, uniqueEvents)
			if err != nil {
				// TODO: error handle
				log.Println("logStore.InsertManyEvent err", err)
				nackAll()
				return
			}

			// Deduplicate deliveries by delivery.ID
			uniqueDeliveries := make([]*models.Delivery, 0, len(deliveryEvents))
			seenDeliveries := make(map[string]struct{})
			for _, deliveryEvent := range deliveryEvents {
				delivery := deliveryEvent.Delivery
				if _, exists := seenDeliveries[delivery.ID]; !exists {
					seenDeliveries[delivery.ID] = struct{}{}
					uniqueDeliveries = append(uniqueDeliveries, delivery)
				}
			}

			err = logStore.InsertManyDelivery(ctx, uniqueDeliveries)
			if err != nil {
				// TODO: error handle
				log.Println("logStore.InsertManyDelivery err", err)
				nackAll()
				return
			}

			for _, msg := range msgs {
				msg.Ack()
			}
		},
	})
	if err != nil {
		log.Println(err)
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
