package deliverymq

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/eventtracer"
	"github.com/hookdeck/EventKit/internal/idempotence"
	"github.com/hookdeck/EventKit/internal/logmq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/hookdeck/EventKit/internal/scheduler"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type messageHandler struct {
	eventTracer    eventtracer.EventTracer
	logger         *otelzap.Logger
	logMQ          *logmq.LogMQ
	entityStore    models.EntityStore
	retryScheduler scheduler.Scheduler
	idempotence    idempotence.Idempotence
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func NewMessageHandler(
	logger *otelzap.Logger,
	redisClient *redis.Client,
	logMQ *logmq.LogMQ,
	entityStore models.EntityStore,
	eventTracer eventtracer.EventTracer,
	retryScheduler scheduler.Scheduler,
) consumer.MessageHandler {
	return &messageHandler{
		eventTracer:    eventTracer,
		logger:         logger,
		logMQ:          logMQ,
		entityStore:    entityStore,
		retryScheduler: retryScheduler,
		idempotence: idempotence.New(redisClient,
			idempotence.WithTimeout(5*time.Second),
			idempotence.WithSuccessfulTTL(24*time.Hour),
		),
	}
}

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	deliveryEvent := models.DeliveryEvent{}
	err := deliveryEvent.FromMessage(msg)
	if err != nil {
		// TODO: question - should we ack this instead?
		// Since we can't parse the message, we won't be able to handle it when retrying later either.
		msg.Nack()
		return err
	}
	idempotenceHandler := func(ctx context.Context) error {
		return h.doHandle(ctx, deliveryEvent)
	}
	err = h.idempotence.Exec(ctx, idempotencyKeyFromDeliveryEvent(deliveryEvent), idempotenceHandler)
	if err != nil {
		// retry if it's an internal error (not a publish error) OR event is eligible for retry
		if _, isPublishErr := err.(*models.DestinationPublishError); !isPublishErr || deliveryEvent.Event.EligibleForRetry {
			if retryErr := h.retryScheduler.Schedule(ctx, deliveryEvent.ID, 1*time.Second); retryErr != nil {
				finalErr := errors.Join(err, retryErr)
				msg.Nack()
				return finalErr
			}
		}
	}
	msg.Ack()
	return err
}

func (h *messageHandler) doHandle(ctx context.Context, deliveryEvent models.DeliveryEvent) error {
	_, span := h.eventTracer.Deliver(ctx, &deliveryEvent)
	defer span.End()
	logger := h.logger.Ctx(ctx)
	logger.Info("deliverymq handler", zap.String("delivery_event", deliveryEvent.ID))
	destination, err := h.entityStore.RetrieveDestination(ctx, deliveryEvent.Event.TenantID, deliveryEvent.DestinationID)
	// TODO: handle destination not found
	// Question: what to do if destination is not found? Nack for later retry?
	if err != nil {
		logger.Error("failed to retrieve destination", zap.Error(err))
		span.RecordError(err)
		return err
	}
	err = destination.Publish(ctx, &deliveryEvent.Event)
	if err != nil {
		logger.Error("failed to publish event", zap.Error(err))
		span.RecordError(err)
		deliveryEvent.Delivery = &models.Delivery{
			ID:              uuid.New().String(),
			DeliveryEventID: deliveryEvent.ID,
			EventID:         deliveryEvent.Event.ID,
			DestinationID:   deliveryEvent.DestinationID,
			Status:          models.DeliveryStatusFailed,
			Time:            time.Now(),
		}
	} else {
		deliveryEvent.Delivery = &models.Delivery{
			ID:              uuid.New().String(),
			DeliveryEventID: deliveryEvent.ID,
			EventID:         deliveryEvent.Event.ID,
			DestinationID:   deliveryEvent.DestinationID,
			Status:          models.DeliveryStatusOK,
			Time:            time.Now(),
		}
	}
	logErr := h.logMQ.Publish(ctx, deliveryEvent)
	if logErr != nil {
		logger.Error("failed to publish log event", zap.Error(err))
		span.RecordError(err)
		err = errors.Join(err, logErr)
	}
	return err
}

func idempotencyKeyFromDeliveryEvent(deliveryEvent models.DeliveryEvent) string {
	return "idempotency:deliverymq:" + deliveryEvent.ID
}
