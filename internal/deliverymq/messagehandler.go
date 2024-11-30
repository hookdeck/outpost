package deliverymq

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/scheduler"
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
	retryMaxCount  int
	idempotence    idempotence.Idempotence
	registry       destregistry.Registry
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func NewMessageHandler(
	logger *otelzap.Logger,
	redisClient *redis.Client,
	logMQ *logmq.LogMQ,
	entityStore models.EntityStore,
	logStore models.LogStore,
	registry destregistry.Registry,
	eventTracer eventtracer.EventTracer,
	retryScheduler scheduler.Scheduler,
	retryBackoff backoff.Backoff,
	retryMaxCount int,
) consumer.MessageHandler {
	return &messageHandler{
		eventTracer:    eventTracer,
		logger:         logger,
		logMQ:          logMQ,
		entityStore:    entityStore,
		logStore:       logStore,
		registry:       registry,
		retryScheduler: retryScheduler,
		retryBackoff:   retryBackoff,
		retryMaxCount:  retryMaxCount,
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
		if h.shouldRetry(err, deliveryEvent) {
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
	provider, err := h.registry.GetProvider(destination.Type)
	if err != nil {
		logger.Error("failed to get destination provider", zap.Error(err))
		span.RecordError(err)
		return err
	}
	var finalErr error
	if err := provider.Publish(ctx, destination, &deliveryEvent.Event); err != nil {
		logger.Error("failed to publish event", zap.Error(err))
		finalErr = err
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
		logger.Error("failed to publish log event", zap.Error(logErr))
		if finalErr == nil {
			finalErr = logErr
		} else {
			finalErr = errors.Join(finalErr, logErr)
		}
	}
	if finalErr != nil {
		span.RecordError(finalErr)
	}
	return finalErr
}

// shouldRetry checks if the event should be retried.
// Qualifications:
// - if the error is an internal error --> should be retried
// - OR if the error is a publish error + event is eligible for retried + the attempt is less than max retry --> should be retried
// Clarification between attemp & max retry: Event.Attempt is zero-based numbering. If Event.Attempt is 4, that means the event has failed 5 times.
// That means 1 original attempt + 4 retries. If max retry is 5, then it should be retried 1 more time. Total attempt will be 6.
//
// Question: what to do if there's an "internal error"? How should that count against the attempt count?
// For example, what if the error happens AFTER deliverying the message (doesn't matter whether it's successful or not),
// say logmq.Publish fails. Should that count as an attempt? What about an error BEFORE deliverying the message?
// Should we write code to differentiate between these two types of errors (predeliveryErr and postdeliveryErr, for example)?
func (h *messageHandler) shouldRetry(err error, deliveryEvent models.DeliveryEvent) bool {
	_, isPublishErr := err.(*destregistry.ErrDestinationPublish)
	if !isPublishErr {
		return true
	}
	return deliveryEvent.Event.EligibleForRetry && deliveryEvent.Attempt < h.retryMaxCount
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
