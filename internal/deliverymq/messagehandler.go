package deliverymq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/scheduler"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func idempotencyKeyFromDeliveryTask(task models.DeliveryTask) string {
	return "idempotency:deliverymq:" + task.IdempotencyKey()
}

var (
	errDestinationDisabled = errors.New("destination disabled")
)

// Error types to distinguish between different stages of delivery
type PreDeliveryError struct {
	err error
}

func (e *PreDeliveryError) Error() string {
	return fmt.Sprintf("pre-delivery error: %v", e.err)
}

func (e *PreDeliveryError) Unwrap() error {
	return e.err
}

type DeliveryError struct {
	err error
}

func (e *DeliveryError) Error() string {
	return fmt.Sprintf("delivery error: %v", e.err)
}

func (e *DeliveryError) Unwrap() error {
	return e.err
}

type PostDeliveryError struct {
	err error
}

func (e *PostDeliveryError) Error() string {
	return fmt.Sprintf("post-delivery error: %v", e.err)
}

func (e *PostDeliveryError) Unwrap() error {
	return e.err
}

type messageHandler struct {
	eventTracer    DeliveryTracer
	logger         *logging.Logger
	logMQ          LogPublisher
	entityStore    DestinationGetter
	logStore       EventGetter
	retryScheduler RetryScheduler
	retryBackoff   backoff.Backoff
	retryMaxLimit  int
	idempotence    idempotence.Idempotence
	publisher      Publisher
	alertMonitor   AlertMonitor
}

type Publisher interface {
	PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) (*models.Delivery, error)
}

type LogPublisher interface {
	Publish(ctx context.Context, entry models.LogEntry) error
}

type RetryScheduler interface {
	Schedule(ctx context.Context, task string, delay time.Duration, opts ...scheduler.ScheduleOption) error
	Cancel(ctx context.Context, taskID string) error
}

type DestinationGetter interface {
	RetrieveDestination(ctx context.Context, tenantID, destID string) (*models.Destination, error)
}

type EventGetter interface {
	RetrieveEvent(ctx context.Context, request logstore.RetrieveEventRequest) (*models.Event, error)
}

type DeliveryTracer interface {
	Deliver(ctx context.Context, task *models.DeliveryTask, destination *models.Destination) (context.Context, trace.Span)
}

type AlertMonitor interface {
	HandleAttempt(ctx context.Context, attempt alert.DeliveryAttempt) error
}

func NewMessageHandler(
	logger *logging.Logger,
	logMQ LogPublisher,
	entityStore DestinationGetter,
	logStore EventGetter,
	publisher Publisher,
	eventTracer DeliveryTracer,
	retryScheduler RetryScheduler,
	retryBackoff backoff.Backoff,
	retryMaxLimit int,
	alertMonitor AlertMonitor,
	idempotence idempotence.Idempotence,
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
		retryMaxLimit:  retryMaxLimit,
		idempotence:    idempotence,
		alertMonitor:   alertMonitor,
	}
}

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	task := models.DeliveryTask{}

	// Parse message
	if err := task.FromMessage(msg); err != nil {
		return h.handleError(msg, &PreDeliveryError{err: err})
	}

	h.logger.Ctx(ctx).Info("processing delivery task",
		zap.String("event_id", task.Event.ID),
		zap.String("tenant_id", task.Event.TenantID),
		zap.String("destination_id", task.DestinationID),
		zap.Int("attempt", task.Attempt))

	// Ensure event data
	if err := h.ensureDeliveryTask(ctx, &task); err != nil {
		return h.handleError(msg, &PreDeliveryError{err: err})
	}

	// Get destination
	destination, err := h.ensurePublishableDestination(ctx, task)
	if err != nil {
		return h.handleError(msg, &PreDeliveryError{err: err})
	}

	// Handle delivery
	err = h.idempotence.Exec(ctx, idempotencyKeyFromDeliveryTask(task), func(ctx context.Context) error {
		return h.doHandle(ctx, task, destination)
	})
	return h.handleError(msg, err)
}

func (h *messageHandler) handleError(msg *mqs.Message, err error) error {
	shouldNack := h.shouldNackError(err)
	if shouldNack {
		msg.Nack()
	} else {
		msg.Ack()
	}

	// Don't return error for expected cases
	var preErr *PreDeliveryError
	if errors.As(err, &preErr) {
		if errors.Is(preErr.err, models.ErrDestinationDeleted) || errors.Is(preErr.err, errDestinationDisabled) {
			return nil
		}
	}
	return err
}

func (h *messageHandler) doHandle(ctx context.Context, task models.DeliveryTask, destination *models.Destination) error {
	_, span := h.eventTracer.Deliver(ctx, &task, destination)
	defer span.End()

	delivery, err := h.publisher.PublishEvent(ctx, destination, &task.Event)
	if err != nil {
		// If delivery is nil, it means no delivery was made.
		// This is an unexpected error and considered a pre-delivery error.
		if delivery == nil {
			return &PreDeliveryError{err: err}
		}

		h.logger.Ctx(ctx).Error("failed to publish event",
			zap.Error(err),
			zap.String("delivery_id", delivery.ID),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type))
		deliveryErr := &DeliveryError{err: err}

		if h.shouldScheduleRetry(task, err) {
			if retryErr := h.scheduleRetry(ctx, task); retryErr != nil {
				return h.logDeliveryResult(ctx, &task, destination, delivery, errors.Join(err, retryErr))
			}
		}
		return h.logDeliveryResult(ctx, &task, destination, delivery, deliveryErr)
	}

	// Handle successful delivery
	if task.Manual {
		logger := h.logger.Ctx(ctx)
		if err := h.retryScheduler.Cancel(ctx, models.RetryID(task.Event.ID, task.DestinationID)); err != nil {
			h.logger.Ctx(ctx).Error("failed to cancel scheduled retry",
				zap.Error(err),
				zap.String("delivery_id", delivery.ID),
				zap.String("event_id", task.Event.ID),
				zap.String("tenant_id", task.Event.TenantID),
				zap.String("destination_id", destination.ID),
				zap.String("destination_type", destination.Type),
				zap.String("retry_id", models.RetryID(task.Event.ID, task.DestinationID)))
			return h.logDeliveryResult(ctx, &task, destination, delivery, err)
		}
		logger.Audit("scheduled retry canceled",
			zap.String("delivery_id", delivery.ID),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type),
			zap.String("retry_id", models.RetryID(task.Event.ID, task.DestinationID)))
	}
	return h.logDeliveryResult(ctx, &task, destination, delivery, nil)
}

func (h *messageHandler) logDeliveryResult(ctx context.Context, task *models.DeliveryTask, destination *models.Destination, delivery *models.Delivery, err error) error {
	logger := h.logger.Ctx(ctx)

	// Set delivery fields from task
	delivery.TenantID = task.Event.TenantID
	delivery.Attempt = task.Attempt
	delivery.Manual = task.Manual

	logger.Audit("event delivered",
		zap.String("delivery_id", delivery.ID),
		zap.String("event_id", task.Event.ID),
		zap.String("tenant_id", task.Event.TenantID),
		zap.String("destination_id", destination.ID),
		zap.String("destination_type", destination.Type),
		zap.String("delivery_status", delivery.Status),
		zap.Int("attempt", task.Attempt),
		zap.Bool("manual", task.Manual))

	// Publish delivery log
	logEntry := models.LogEntry{
		Event:    &task.Event,
		Delivery: delivery,
	}
	if logErr := h.logMQ.Publish(ctx, logEntry); logErr != nil {
		logger.Error("failed to publish delivery log",
			zap.Error(logErr),
			zap.String("delivery_id", delivery.ID),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type))
		if err != nil {
			return &PostDeliveryError{err: errors.Join(err, logErr)}
		}
		return &PostDeliveryError{err: logErr}
	}

	// Call alert monitor in goroutine
	go h.handleAlertAttempt(ctx, task, destination, delivery, err)

	// If we have a DeliveryError, return it as is
	var delErr *DeliveryError
	if errors.As(err, &delErr) {
		return err
	}

	// If we have a PreDeliveryError, return it as is
	var preErr *PreDeliveryError
	if errors.As(err, &preErr) {
		return err
	}

	// For any other error, wrap it in PostDeliveryError
	if err != nil {
		return &PostDeliveryError{err: err}
	}

	return nil
}

func (h *messageHandler) handleAlertAttempt(ctx context.Context, task *models.DeliveryTask, destination *models.Destination, delivery *models.Delivery, err error) {
	attempt := alert.DeliveryAttempt{
		Success:      delivery.Status == models.DeliveryStatusSuccess,
		DeliveryTask: task,
		Destination: &alert.AlertDestination{
			ID:         destination.ID,
			TenantID:   destination.TenantID,
			Type:       destination.Type,
			Topics:     destination.Topics,
			Config:     destination.Config,
			CreatedAt:  destination.CreatedAt,
			DisabledAt: destination.DisabledAt,
		},
		Timestamp: delivery.Time,
	}

	if !attempt.Success && err != nil {
		// Extract attempt data if available
		var delErr *DeliveryError
		if errors.As(err, &delErr) {
			var pubErr *destregistry.ErrDestinationPublishAttempt
			if errors.As(delErr.err, &pubErr) {
				attempt.DeliveryResponse = pubErr.Data
			} else {
				attempt.DeliveryResponse = map[string]interface{}{
					"error": delErr.err.Error(),
				}
			}
		} else {
			attempt.DeliveryResponse = map[string]interface{}{
				"error":   "unexpected",
				"message": err.Error(),
			}
		}
	}

	if monitorErr := h.alertMonitor.HandleAttempt(ctx, attempt); monitorErr != nil {
		h.logger.Ctx(ctx).Error("failed to handle alert attempt",
			zap.Error(monitorErr),
			zap.String("delivery_id", delivery.ID),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", destination.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type))
		return
	}

	h.logger.Ctx(ctx).Info("alert attempt handled",
		zap.String("delivery_id", delivery.ID),
		zap.String("event_id", task.Event.ID),
		zap.String("tenant_id", destination.TenantID),
		zap.String("destination_id", destination.ID),
		zap.String("destination_type", destination.Type))
}

func (h *messageHandler) shouldScheduleRetry(task models.DeliveryTask, err error) bool {
	if task.Manual {
		return false
	}
	if !task.Event.EligibleForRetry {
		return false
	}
	if _, ok := err.(*destregistry.ErrDestinationPublishAttempt); !ok {
		return false
	}
	// Attempt starts at 0 for initial attempt, so we can compare directly
	return task.Attempt < h.retryMaxLimit
}

func (h *messageHandler) shouldNackError(err error) bool {
	if err == nil {
		return false // Success case, always ack
	}

	// Handle pre-delivery errors (system errors)
	var preErr *PreDeliveryError
	if errors.As(err, &preErr) {
		// Don't nack if it's a permanent error
		if errors.Is(preErr.err, models.ErrDestinationDeleted) || errors.Is(preErr.err, errDestinationDisabled) {
			return false
		}
		return true // Nack other pre-delivery errors
	}

	// Handle delivery errors
	var delErr *DeliveryError
	if errors.As(err, &delErr) {
		return h.shouldNackDeliveryError(delErr.err)
	}

	// Handle post-delivery errors
	var postErr *PostDeliveryError
	if errors.As(err, &postErr) {
		// Check if this wraps a delivery error
		var delErr *DeliveryError
		if errors.As(postErr.err, &delErr) {
			return h.shouldNackDeliveryError(delErr.err)
		}
		return true // Nack other post-delivery errors
	}

	// For any other error type, nack for safety
	return true
}

func (h *messageHandler) shouldNackDeliveryError(err error) bool {
	// Don't nack if it's a delivery attempt error (handled by retry scheduling)
	if _, ok := err.(*destregistry.ErrDestinationPublishAttempt); ok {
		return false
	}
	return true // Nack other delivery errors
}

func (h *messageHandler) scheduleRetry(ctx context.Context, task models.DeliveryTask) error {
	backoffDuration := h.retryBackoff.Duration(task.Attempt)

	retryTask := RetryTaskFromDeliveryTask(task)
	retryTaskStr, err := retryTask.ToString()
	if err != nil {
		return err
	}

	if err := h.retryScheduler.Schedule(ctx, retryTaskStr, backoffDuration, scheduler.WithTaskID(models.RetryID(task.Event.ID, task.DestinationID))); err != nil {
		h.logger.Ctx(ctx).Error("failed to schedule retry",
			zap.Error(err),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", task.DestinationID),
			zap.Int("attempt", task.Attempt),
			zap.Duration("backoff", backoffDuration))
		return err
	}

	h.logger.Ctx(ctx).Audit("retry scheduled",
		zap.String("event_id", task.Event.ID),
		zap.String("tenant_id", task.Event.TenantID),
		zap.String("destination_id", task.DestinationID),
		zap.Int("attempt", task.Attempt),
		zap.Duration("backoff", backoffDuration))

	return nil
}

// ensureDeliveryTask ensures that the delivery task has full event data.
// In retry scenarios, the task only has event ID and we'll need to query the full data.
func (h *messageHandler) ensureDeliveryTask(ctx context.Context, task *models.DeliveryTask) error {
	// TODO: consider a more deliberate way to check for retry scenario?
	if !task.Event.Time.IsZero() {
		return nil
	}

	event, err := h.logStore.RetrieveEvent(ctx, logstore.RetrieveEventRequest{
		TenantID: task.Event.TenantID,
		EventID:  task.Event.ID,
	})
	if err != nil {
		return err
	}
	if event == nil {
		return errors.New("event not found")
	}
	task.Event = *event

	return nil
}

// ensurePublishableDestination ensures that the destination exists and is in a publishable state.
// Returns an error if the destination is not found, deleted, disabled, or any other state that
// would prevent publishing.
func (h *messageHandler) ensurePublishableDestination(ctx context.Context, task models.DeliveryTask) (*models.Destination, error) {
	destination, err := h.entityStore.RetrieveDestination(ctx, task.Event.TenantID, task.DestinationID)
	if err != nil {
		logger := h.logger.Ctx(ctx)
		fields := []zap.Field{
			zap.Error(err),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", task.DestinationID),
		}

		if errors.Is(err, models.ErrDestinationDeleted) {
			logger.Info("destination deleted", fields...)
		} else {
			// Unexpected errors like DB connection issues
			logger.Error("failed to retrieve destination", fields...)
		}
		return nil, err
	}
	if destination == nil {
		h.logger.Ctx(ctx).Info("destination not found",
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", task.DestinationID))
		return nil, models.ErrDestinationNotFound
	}
	if destination.DisabledAt != nil {
		h.logger.Ctx(ctx).Info("skipping disabled destination",
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type),
			zap.Time("disabled_at", *destination.DisabledAt))
		return nil, errDestinationDisabled
	}
	return destination, nil
}
