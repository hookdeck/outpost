package deliverymq

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var (
	errDestinationDisabled = errors.New("destination disabled")
)

type messageHandler struct {
	eventTracer    DeliveryTracer
	logger         *otelzap.Logger
	logMQ          LogPublisher
	entityStore    DestinationGetter
	logStore       EventGetter
	retryScheduler scheduler.Scheduler
	retryBackoff   backoff.Backoff
	retryMaxCount  int
	idempotence    idempotence.Idempotence
	publisher      Publisher
}

type Publisher interface {
	PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) error
}

type LogPublisher interface {
	Publish(ctx context.Context, deliveryEvent models.DeliveryEvent) error
}

type DestinationGetter interface {
	RetrieveDestination(ctx context.Context, tenantID, destID string) (*models.Destination, error)
}

type EventGetter interface {
	RetrieveEvent(ctx context.Context, tenantID, eventID string) (*models.Event, error)
}

type DeliveryTracer interface {
	Deliver(ctx context.Context, deliveryEvent *models.DeliveryEvent) (context.Context, trace.Span)
}

func NewMessageHandler(
	logger *otelzap.Logger,
	redisClient *redis.Client,
	logMQ LogPublisher,
	entityStore DestinationGetter,
	logStore EventGetter,
	publisher Publisher,
	eventTracer DeliveryTracer,
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
		publisher:      publisher,
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
	if err := h.idempotence.Exec(ctx, idempotencyKeyFromDeliveryEvent(deliveryEvent), idempotenceHandler); err != nil {
		shouldScheduleRetry, shouldNack := h.checkError(err, deliveryEvent)
		if shouldScheduleRetry {
			if retryErr := h.scheduleRetry(ctx, deliveryEvent); retryErr != nil {
				finalErr := errors.Join(err, retryErr)
				msg.Nack()
				return finalErr
			}
		}
		if shouldNack {
			msg.Nack()
		} else {
			msg.Ack()
		}
		return err
	}
	msg.Ack()
	return nil
}

func (h *messageHandler) doHandle(ctx context.Context, deliveryEvent models.DeliveryEvent) error {
	_, span := h.eventTracer.Deliver(ctx, &deliveryEvent)
	defer span.End()
	logger := h.logger.Ctx(ctx)
	logger.Info("deliverymq handler", zap.String("delivery_event", deliveryEvent.ID))

	destination, err := h.ensurePublishableDestination(ctx, deliveryEvent)
	if err != nil {
		span.RecordError(err)
		return err
	}

	var finalErr error
	if err := h.publisher.PublishEvent(ctx, destination, &deliveryEvent.Event); err != nil {
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

// QUESTION: What if an internal error happens AFTER deliverying the message (doesn't matter whether it's successful or not),
// say logmq.Publish fails. Should that count as an attempt? What about an error BEFORE deliverying the message?
// Should we write code to differentiate between these two types of errors (predeliveryErr and postdeliveryErr, for example)?
func (h *messageHandler) checkError(err error, deliveryEvent models.DeliveryEvent) (shouldScheduleRetry, shouldNack bool) {
	if errors.Is(err, models.ErrDestinationDeleted) || errors.Is(err, errDestinationDisabled) {
		return false, false // ack
	}

	if _, ok := err.(*destregistry.ErrDestinationPublishAttempt); ok {
		if deliveryEvent.Event.EligibleForRetry && deliveryEvent.Attempt < h.retryMaxCount {
			return true, false // schedule retry and ack
		}
		return false, false // ack and accept failure
	}

	return false, true // nack for system retry
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

// ensurePublishableDestination ensures that the destination exists and is in a publishable state.
// Returns an error if the destination is not found, deleted, disabled, or any other state that
// would prevent publishing.
func (h *messageHandler) ensurePublishableDestination(ctx context.Context, deliveryEvent models.DeliveryEvent) (*models.Destination, error) {
	logger := h.logger.Ctx(ctx)
	destination, err := h.entityStore.RetrieveDestination(ctx, deliveryEvent.Event.TenantID, deliveryEvent.DestinationID)
	if err != nil {
		logger.Error("failed to retrieve destination", zap.Error(err))
		return nil, err
	}
	if destination == nil {
		logger.Error("destination not found")
		return nil, models.ErrDestinationNotFound
	}
	if destination.DisabledAt != nil {
		logger.Info("destination is disabled", zap.String("destination_id", destination.ID))
		return nil, errDestinationDisabled
	}
	return destination, nil
}
