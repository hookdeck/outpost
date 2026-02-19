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
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/tenantstore"
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

type AttemptError struct {
	err error
}

func (e *AttemptError) Error() string {
	return fmt.Sprintf("attempt error: %v", e.err)
}

func (e *AttemptError) Unwrap() error {
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
	tenantStore    DestinationGetter
	retryScheduler RetryScheduler
	retryBackoff   backoff.Backoff
	retryMaxLimit  int
	idempotence    idempotence.Idempotence
	publisher      Publisher
	alertMonitor   AlertMonitor
}

type Publisher interface {
	PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) (*models.Attempt, error)
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

type DeliveryTracer interface {
	Deliver(ctx context.Context, task *models.DeliveryTask, destination *models.Destination) (context.Context, trace.Span)
}

type AlertMonitor interface {
	HandleAttempt(ctx context.Context, attempt alert.DeliveryAttempt) error
}

func NewMessageHandler(
	logger *logging.Logger,
	logMQ LogPublisher,
	tenantStore DestinationGetter,
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
		tenantStore:    tenantStore,
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

	if err := task.FromMessage(msg); err != nil {
		return h.handleError(msg, &PreDeliveryError{err: err})
	}

	h.logger.Ctx(ctx).Info("processing delivery task",
		zap.String("event_id", task.Event.ID),
		zap.String("tenant_id", task.Event.TenantID),
		zap.String("destination_id", task.DestinationID),
		zap.Int("attempt", task.Attempt))

	destination, err := h.ensurePublishableDestination(ctx, task)
	if err != nil {
		return h.handleError(msg, &PreDeliveryError{err: err})
	}

	executed := false
	idempotencyKey := idempotencyKeyFromDeliveryTask(task)
	err = h.idempotence.Exec(ctx, idempotencyKey, func(ctx context.Context) error {
		executed = true
		return h.doHandle(ctx, task, destination)
	})
	if err == nil && !executed {
		h.logger.Ctx(ctx).Info("delivery task skipped (idempotent)",
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", task.DestinationID),
			zap.Int("attempt", task.Attempt),
			zap.Bool("manual", task.Manual),
			zap.String("idempotency_key", idempotencyKey))
	}
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
		if errors.Is(preErr.err, tenantstore.ErrDestinationDeleted) || errors.Is(preErr.err, errDestinationDisabled) {
			return nil
		}
	}
	return err
}

func (h *messageHandler) doHandle(ctx context.Context, task models.DeliveryTask, destination *models.Destination) error {
	_, span := h.eventTracer.Deliver(ctx, &task, destination)
	defer span.End()

	attempt, err := h.publisher.PublishEvent(ctx, destination, &task.Event)
	if err != nil {
		// If attempt is nil, it means no attempt was made.
		// This is an unexpected error and considered a pre-delivery error.
		if attempt == nil {
			return &PreDeliveryError{err: err}
		}

		h.logger.Ctx(ctx).Error("failed to publish event",
			zap.Error(err),
			zap.String("attempt_id", attempt.ID),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type))
		attemptErr := &AttemptError{err: err}

		if h.shouldScheduleRetry(task, err) {
			if retryErr := h.scheduleRetry(ctx, task); retryErr != nil {
				return h.logDeliveryResult(ctx, &task, destination, attempt, errors.Join(err, retryErr))
			}
		}
		return h.logDeliveryResult(ctx, &task, destination, attempt, attemptErr)
	}

	// Handle successful delivery
	if task.Manual {
		logger := h.logger.Ctx(ctx)
		if err := h.retryScheduler.Cancel(ctx, models.RetryID(task.Event.ID, task.DestinationID)); err != nil {
			h.logger.Ctx(ctx).Error("failed to cancel scheduled retry",
				zap.Error(err),
				zap.String("attempt_id", attempt.ID),
				zap.String("event_id", task.Event.ID),
				zap.String("tenant_id", task.Event.TenantID),
				zap.String("destination_id", destination.ID),
				zap.String("destination_type", destination.Type),
				zap.String("retry_id", models.RetryID(task.Event.ID, task.DestinationID)))
			return h.logDeliveryResult(ctx, &task, destination, attempt, err)
		}
		logger.Audit("scheduled retry canceled",
			zap.String("attempt_id", attempt.ID),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type),
			zap.String("retry_id", models.RetryID(task.Event.ID, task.DestinationID)))
	}
	return h.logDeliveryResult(ctx, &task, destination, attempt, nil)
}

func (h *messageHandler) logDeliveryResult(ctx context.Context, task *models.DeliveryTask, destination *models.Destination, attempt *models.Attempt, err error) error {
	logger := h.logger.Ctx(ctx)

	attempt.TenantID = task.Event.TenantID
	attempt.AttemptNumber = task.Attempt
	attempt.Manual = task.Manual

	logger.Audit("event delivered",
		zap.String("attempt_id", attempt.ID),
		zap.String("event_id", task.Event.ID),
		zap.String("tenant_id", task.Event.TenantID),
		zap.String("destination_id", destination.ID),
		zap.String("destination_type", destination.Type),
		zap.String("attempt_status", attempt.Status),
		zap.Int("attempt", task.Attempt),
		zap.Bool("manual", task.Manual))

	logEntry := models.LogEntry{
		Event:   &task.Event,
		Attempt: attempt,
	}
	if logErr := h.logMQ.Publish(ctx, logEntry); logErr != nil {
		logger.Error("failed to publish attempt log",
			zap.Error(logErr),
			zap.String("attempt_id", attempt.ID),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type))
		if err != nil {
			return &PostDeliveryError{err: errors.Join(err, logErr)}
		}
		return &PostDeliveryError{err: logErr}
	}

	go h.handleAlertAttempt(ctx, destination, &task.Event, attempt)

	// If we have an AttemptError, return it as is
	var atmErr *AttemptError
	if errors.As(err, &atmErr) {
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

func (h *messageHandler) handleAlertAttempt(ctx context.Context, destination *models.Destination, event *models.Event, attempt *models.Attempt) {
	da := alert.DeliveryAttempt{
		Event:       event,
		Destination: alert.AlertDestinationFromDestination(destination),
		Attempt:     attempt,
	}

	if monitorErr := h.alertMonitor.HandleAttempt(ctx, da); monitorErr != nil {
		h.logger.Ctx(ctx).Error("failed to handle alert attempt",
			zap.Error(monitorErr),
			zap.String("attempt_id", attempt.ID),
			zap.String("event_id", event.ID),
			zap.String("tenant_id", destination.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type))
		return
	}

	h.logger.Ctx(ctx).Info("alert attempt handled",
		zap.String("attempt_id", attempt.ID),
		zap.String("event_id", event.ID),
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
	var pubErr *destregistry.ErrDestinationPublishAttempt
	if !errors.As(err, &pubErr) {
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
		if errors.Is(preErr.err, tenantstore.ErrDestinationDeleted) || errors.Is(preErr.err, errDestinationDisabled) {
			return false
		}
		return true // Nack other pre-delivery errors
	}

	// Handle delivery errors
	var atmErr *AttemptError
	if errors.As(err, &atmErr) {
		return h.shouldNackDeliveryError(atmErr.err)
	}

	// Handle post-delivery errors
	var postErr *PostDeliveryError
	if errors.As(err, &postErr) {
		// Check if this wraps a delivery error
		var atmErr2 *AttemptError
		if errors.As(postErr.err, &atmErr2) {
			return h.shouldNackDeliveryError(atmErr2.err)
		}
		return true // Nack other post-delivery errors
	}

	// For any other error type, nack for safety
	return true
}

func (h *messageHandler) shouldNackDeliveryError(err error) bool {
	// Don't nack if it's a delivery attempt error (handled by retry scheduling)
	var pubErr *destregistry.ErrDestinationPublishAttempt
	if errors.As(err, &pubErr) {
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

// ensurePublishableDestination ensures that the destination exists and is in a publishable state.
// Returns an error if the destination is not found, deleted, disabled, or any other state that
// would prevent publishing.
func (h *messageHandler) ensurePublishableDestination(ctx context.Context, task models.DeliveryTask) (*models.Destination, error) {
	destination, err := h.tenantStore.RetrieveDestination(ctx, task.Event.TenantID, task.DestinationID)
	if err != nil {
		logger := h.logger.Ctx(ctx)
		fields := []zap.Field{
			zap.Error(err),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", task.DestinationID),
		}

		if errors.Is(err, tenantstore.ErrDestinationDeleted) {
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
		return nil, tenantstore.ErrDestinationNotFound
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
