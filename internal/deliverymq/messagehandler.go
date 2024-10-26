package deliverymq

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/backoff"
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
	logStore       models.LogStore
	retryScheduler scheduler.Scheduler
	retryBackoff   backoff.Backoff
	idempotence    idempotence.Idempotence
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func NewMessageHandler(
	logger *otelzap.Logger,
	redisClient *redis.Client,
	logMQ *logmq.LogMQ,
	entityStore models.EntityStore,
	logStore models.LogStore,
	eventTracer eventtracer.EventTracer,
	retryScheduler scheduler.Scheduler,
	retryBackoff backoff.Backoff,
) consumer.MessageHandler {
	return &messageHandler{
		eventTracer:    eventTracer,
		logger:         logger,
		logMQ:          logMQ,
		entityStore:    entityStore,
		logStore:       logStore,
		retryScheduler: retryScheduler,
		retryBackoff:   retryBackoff,
		idempotence: idempotence.New(redisClient,
			idempotence.WithTimeout(5*time.Second),
			idempotence.WithSuccessfulTTL(24*time.Hour),
		),
	}
}

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	deliveryEvent := models.DeliveryEvent{}
	if err := deliveryEvent.FromMessage(msg); err != nil {
		msg.Nack()
		return err
	}
	if err := h.ensureDeliveryEvent(ctx, &deliveryEvent); err != nil {
		msg.Nack()
		return err
	}
	idempotenceHandler := func(ctx context.Context) error {
		return h.doHandle(ctx, deliveryEvent)
	}
	err := h.idempotence.Exec(ctx, idempotencyKeyFromDeliveryEvent(deliveryEvent), idempotenceHandler)
	if err != nil {
		// retry if it's an internal error (not a publish error) OR event is eligible for retry
		if _, isPublishErr := err.(*models.DestinationPublishError); !isPublishErr || deliveryEvent.Event.EligibleForRetry {
			if retryErr := h.scheduleRetry(ctx, deliveryEvent); retryErr != nil {
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
	if err != nil {
		logger.Error("failed to retrieve destination", zap.Error(err))
		span.RecordError(err)
		return err
	}
	if destination == nil {
		logger.Error("destination not found")
		span.RecordError(errors.New("destination not found"))
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

func (h *messageHandler) scheduleRetry(ctx context.Context, deliveryEvent models.DeliveryEvent) error {
	retryMessage := RetryMessageFromDeliveryEvent(deliveryEvent)
	retryMessageStr, err := retryMessage.ToString()
	if err != nil {
		return err
	}
	return h.retryScheduler.Schedule(ctx, retryMessageStr, h.retryBackoff.Duration(deliveryEvent.Attempt))
}

// ensureDeliveryEvent ensures that the delivery event struct has full data.
// In retry scenarios, the delivery event only has its ID and we'll need to query the full data.
func (h *messageHandler) ensureDeliveryEvent(ctx context.Context, deliveryEvent *models.DeliveryEvent) error {
	// TODO: consider a more deliberate way to check for retry scenario?
	if !deliveryEvent.Event.Time.IsZero() {
		return nil
	}

	event, err := h.logStore.RetrieveEvent(ctx, deliveryEvent.Event.TenantID, deliveryEvent.Event.ID)
	if err != nil {
		return err
	}
	if event == nil {
		return errors.New("event not found")
	}
	deliveryEvent.Event = *event

	return nil
}

func idempotencyKeyFromDeliveryEvent(deliveryEvent models.DeliveryEvent) string {
	return "idempotency:deliverymq:" + deliveryEvent.ID
}
