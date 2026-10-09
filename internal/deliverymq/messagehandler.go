package deliverymq

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	// errDestinationExpired: the destination's expires_at has passed. No
	// attempt (first or retry) is made after expiry.
	errDestinationExpired = errors.New("destination expired")
	// errDestinationGenerationMismatch: a retry scheduled for an earlier
	// destination that was deleted and recreated under the same ID.
	errDestinationGenerationMismatch = errors.New("destination generation mismatch")
	// errRetryParked: the retry was parked on its disabled destination, to be
	// resumed when the destination is re-enabled.
	errRetryParked = errors.New("retry parked on disabled destination")
	// errDestinationDisabledAgain: parking found the destination enabled, but
	// the re-read shows it disabled again. Rare; nacked so the redelivery
	// re-evaluates (and parks) instead of dropping the retry.
	errDestinationDisabledAgain = errors.New("destination re-disabled while parking retry")
)

// isPermanentPreDeliveryErr reports whether a pre-delivery error settles the
// task for good: the message is acked and the error is not surfaced to the
// consumer. Every other pre-delivery error nacks.
func isPermanentPreDeliveryErr(err error) bool {
	return errors.Is(err, tenantstore.ErrDestinationDeleted) ||
		errors.Is(err, errDestinationDisabled) ||
		errors.Is(err, errDestinationExpired) ||
		errors.Is(err, errDestinationGenerationMismatch) ||
		errors.Is(err, errRetryParked)
}

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

	// Per-destination-type behaviour, set through MessageHandlerOptions.
	retryPolicies     map[string]retryPolicy
	generationChecked map[string]bool
	retryParker       RetryParker
	parkedRetryTypes  map[string]bool

	now       func() time.Time
	isExpired func(destination *models.Destination, now time.Time) bool
}

// retryPolicy is the retry schedule for one destination type: the backoff
// before each retry and the number of retries after the first attempt.
type retryPolicy struct {
	backoff  backoff.Backoff
	maxLimit int
}

// RetryParker parks a retry whose destination is disabled so it can be
// resumed when the destination is re-enabled, instead of being dropped.
//
// ParkRetry must re-check the stored destination atomically with parking:
// park only while it is live and disabled. parked=false with a nil error means
// the destination is no longer disabled (or no longer live); the handler then
// re-reads it and proceeds as for any other task. An error nacks the task.
type RetryParker interface {
	ParkRetry(ctx context.Context, task RetryTask, destination *models.Destination) (parked bool, err error)
}

// MessageHandlerOption configures optional, per-destination-type behaviour of
// the delivery handler.
type MessageHandlerOption func(*messageHandler)

// WithRetryPolicy gives one destination type its own retry schedule in place of
// the handler's default backoff and max limit: the backoff before each retry
// (nil keeps the default backoff) and the number of retries after the first
// attempt (negative = 0, no retries). It drives retry scheduling and the
// attempt_max log field.
func WithRetryPolicy(destinationType string, b backoff.Backoff, maxLimit int) MessageHandlerOption {
	return func(h *messageHandler) {
		if h.retryPolicies == nil {
			h.retryPolicies = make(map[string]retryPolicy)
		}
		h.retryPolicies[destinationType] = retryPolicy{backoff: b, maxLimit: max(maxLimit, 0)}
	}
}

// WithGenerationCheckedTypes marks destination types whose IDs can be reused
// after deletion (deterministic IDs). Retries scheduled for them carry the
// destination's CreatedAt and are dropped when it no longer matches, so a
// pending retry never reaches a newer destination that took over the ID.
func WithGenerationCheckedTypes(destinationTypes ...string) MessageHandlerOption {
	return func(h *messageHandler) {
		if h.generationChecked == nil {
			h.generationChecked = make(map[string]bool, len(destinationTypes))
		}
		for _, t := range destinationTypes {
			h.generationChecked[t] = true
		}
	}
}

// WithRetryParker sets the parker used for WithParkedRetryTypes.
func WithRetryParker(parker RetryParker) MessageHandlerOption {
	return func(h *messageHandler) {
		h.retryParker = parker
	}
}

// WithParkedRetryTypes makes automatic retries (attempt > 1, not manual) of the
// given destination types park on a disabled destination instead of being
// dropped. Needs WithRetryParker; first attempts and manual retries are never
// parked.
func WithParkedRetryTypes(destinationTypes ...string) MessageHandlerOption {
	return func(h *messageHandler) {
		if h.parkedRetryTypes == nil {
			h.parkedRetryTypes = make(map[string]bool, len(destinationTypes))
		}
		for _, t := range destinationTypes {
			h.parkedRetryTypes[t] = true
		}
	}
}

// expirable is implemented by destinations that carry an expiry.
// destinationExpired reports whether the destination's expiry has passed.
// Destinations without an expiry never expire.
func destinationExpired(destination *models.Destination, now time.Time) bool {
	return destination.IsExpired(now)
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

func NewMessageHandler(
	logger *logging.Logger,
	logMQ LogPublisher,
	tenantStore DestinationGetter,
	publisher Publisher,
	eventTracer DeliveryTracer,
	retryScheduler RetryScheduler,
	retryBackoff backoff.Backoff,
	retryMaxLimit int,
	idempotence idempotence.Idempotence,
	opts ...MessageHandlerOption,
) consumer.MessageHandler {
	h := &messageHandler{
		eventTracer:    eventTracer,
		logger:         logger,
		logMQ:          logMQ,
		tenantStore:    tenantStore,
		publisher:      publisher,
		retryScheduler: retryScheduler,
		retryBackoff:   retryBackoff,
		retryMaxLimit:  retryMaxLimit,
		idempotence:    idempotence,
		now:            time.Now,
		isExpired:      destinationExpired,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	task := models.DeliveryTask{}

	if err := task.FromMessage(msg); err != nil {
		return h.handleError(msg, &PreDeliveryError{err: err})
	}

	h.logger.Ctx(ctx).Debug("processing delivery task",
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
		h.logger.Ctx(ctx).Debug("delivery task skipped (idempotent)",
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
		if isPermanentPreDeliveryErr(preErr.err) {
			return nil
		}
	}
	// Attempt errors from a destination's publish call (webhook 5xx, timeout,
	// refused, etc.) are expected operational outcomes. Ack semantics are
	// already decided above; the failure is captured in the delivery.attempted
	// line, the ClickHouse log entry, and the scheduled retry. Suppress
	// propagation so the consumer doesn't log them as unexpected handler errors.
	if atmErr, ok := err.(*AttemptError); ok {
		var pubErr *destregistry.ErrDestinationPublishAttempt
		if errors.As(atmErr.err, &pubErr) {
			return nil
		}
	}
	return err
}

// retryOutcome defers the retry-scheduling decisions taken during an attempt
// so logDeliveryResult can fold them into the one delivery.attempted line.
type retryOutcome struct {
	scheduled      bool
	backoff        time.Duration
	scheduleFailed bool
	canceled       bool
	cancelFailed   bool
}

func (h *messageHandler) doHandle(ctx context.Context, task models.DeliveryTask, destination *models.Destination) error {
	_, span := h.eventTracer.Deliver(ctx, &task, destination)
	defer span.End()

	attemptStart := time.Now()
	attempt, err := h.publisher.PublishEvent(ctx, destination, &task.Event)
	attemptDuration := time.Since(attemptStart)

	var retry retryOutcome

	if err != nil {
		// If attempt is nil, it means no attempt was made.
		// This is an unexpected error and considered a pre-delivery error.
		if attempt == nil {
			return &PreDeliveryError{err: err}
		}

		// Record delivery failure for metrics
		if recorder, ok := span.(interface{ RecordDeliveryResult(bool) }); ok {
			recorder.RecordDeliveryResult(false)
		}

		attemptErr := &AttemptError{err: err}

		if h.shouldScheduleRetry(task, destination, err) {
			// scheduleRetry uses RetryID (event_id:destination_id) as the scheduler
			// task ID. The scheduler has upsert semantics: scheduling with the same ID
			// atomically replaces the existing entry (both timing and payload). This
			// means manual retries automatically override any pending automatic retry
			// without needing an explicit cancel — the new tier's delay takes effect
			// and the old scheduled retry is gone in a single operation.
			backoff, retryErr := h.scheduleRetry(ctx, task, destination)
			retry.backoff = backoff
			if retryErr != nil {
				retry.scheduleFailed = true
				return h.logDeliveryResult(ctx, &task, destination, attempt, attemptStart, attemptDuration, retry, errors.Join(err, retryErr))
			}
			retry.scheduled = true
		} else if task.Manual {
			// Budget exhausted, not eligible or non-retryable — cancel any lingering scheduled retry.
			// Unlike the case above, there's no new retry to schedule so we must
			// explicitly cancel to prevent a stale automatic retry from firing.
			if cancelErr := h.retryScheduler.Cancel(ctx, models.RetryID(task.Event.ID, task.DestinationID)); cancelErr == nil {
				retry.canceled = true
			} else {
				retry.cancelFailed = true
			}
		}
		return h.logDeliveryResult(ctx, &task, destination, attempt, attemptStart, attemptDuration, retry, attemptErr)
	}

	// Record delivery success for metrics
	if recorder, ok := span.(interface{ RecordDeliveryResult(bool) }); ok {
		recorder.RecordDeliveryResult(true)
	}

	// Handle successful delivery
	if task.Manual {
		if cancelErr := h.retryScheduler.Cancel(ctx, models.RetryID(task.Event.ID, task.DestinationID)); cancelErr != nil {
			retry.cancelFailed = true
			h.logger.Ctx(ctx).Error("failed to cancel scheduled retry",
				zap.Error(cancelErr),
				zap.String("attempt_id", attempt.ID),
				zap.String("event_id", task.Event.ID),
				zap.String("tenant_id", task.Event.TenantID),
				zap.String("destination_id", destination.ID),
				zap.String("destination_type", destination.Type),
				zap.String("retry_id", models.RetryID(task.Event.ID, task.DestinationID)))
			return h.logDeliveryResult(ctx, &task, destination, attempt, attemptStart, attemptDuration, retry, cancelErr)
		}
		retry.canceled = true
	}
	return h.logDeliveryResult(ctx, &task, destination, attempt, attemptStart, attemptDuration, retry, nil)
}

func (h *messageHandler) logDeliveryResult(ctx context.Context, task *models.DeliveryTask, destination *models.Destination, attempt *models.Attempt, attemptStart time.Time, attemptDuration time.Duration, retry retryOutcome, err error) error {
	logger := h.logger.Ctx(ctx)

	attempt.TenantID = task.Event.TenantID
	attempt.AttemptNumber = task.Attempt
	attempt.Manual = task.Manual

	fields := []zap.Field{
		zap.String("attempt_id", attempt.ID),
		zap.String("event_id", task.Event.ID),
		zap.String("tenant_id", task.Event.TenantID),
		zap.String("topic", task.Event.Topic),
		zap.String("destination_id", destination.ID),
		zap.String("destination_type", destination.Type),
		zap.String("attempt_status", attempt.Status),
		zap.String("attempt_code", attempt.Code),
		zap.Int("attempt_number", task.Attempt),
		zap.Int("attempt_max", h.retryPolicyFor(destination.Type).maxLimit+1),
		zap.Bool("manual", task.Manual),
		zap.Bool("eligible_for_retry", task.Event.EligibleForRetry),
		zap.Time("attempt_started_at", attemptStart),
		zap.Int64("attempt_duration_ms", attemptDuration.Milliseconds()),
		zap.String("retry_id", models.RetryID(task.Event.ID, task.DestinationID)),
		zap.Bool("retry_scheduled", retry.scheduled),
		zap.Bool("retry_canceled", retry.canceled),
	}
	if retry.scheduled {
		fields = append(fields, zap.Int64("retry_backoff_ms", retry.backoff.Milliseconds()))
	}
	if retry.scheduleFailed {
		fields = append(fields, zap.Bool("retry_schedule_failed", true))
	}
	if retry.cancelFailed {
		fields = append(fields, zap.Bool("retry_cancel_failed", true))
	}
	if destregistry.IsNonRetryable(err) {
		fields = append(fields, zap.Bool("non_retryable", true))
	}
	logger.Info("delivery.attempted", fields...)

	// The destination rides along for alert evaluation in logmq, which never
	// needs credentials: publish a copy without them so secrets don't transit
	// the log queue. The caller's destination is left untouched.
	logDestination := *destination
	logDestination.Credentials = nil
	logEntry := models.LogEntry{
		Event:       &task.Event,
		Attempt:     attempt,
		Destination: &logDestination,
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

func (h *messageHandler) shouldScheduleRetry(task models.DeliveryTask, destination *models.Destination, err error) bool {
	if !task.Event.EligibleForRetry {
		return false
	}
	var pubErr *destregistry.ErrDestinationPublishAttempt
	if !errors.As(err, &pubErr) {
		return false
	}
	if pubErr.NonRetryable {
		return false
	}
	// Attempt is 1-indexed: max attempts = 1 (initial) + maxLimit (retries)
	return task.Attempt <= h.retryPolicyFor(destination.Type).maxLimit
}

// retryPolicyFor returns the destination type's retry policy, falling back to
// the handler's default backoff and max limit.
func (h *messageHandler) retryPolicyFor(destinationType string) retryPolicy {
	p, ok := h.retryPolicies[destinationType]
	if !ok {
		return retryPolicy{backoff: h.retryBackoff, maxLimit: h.retryMaxLimit}
	}
	if p.backoff == nil {
		p.backoff = h.retryBackoff
	}
	return p
}

// retryTaskFor builds the retry task for a delivery task, stamping the
// destination generation for generation-checked types.
func (h *messageHandler) retryTaskFor(task models.DeliveryTask, destination *models.Destination) RetryTask {
	retryTask := RetryTaskFromDeliveryTask(task)
	if h.generationChecked[destination.Type] && !destination.CreatedAt.IsZero() {
		retryTask.DestinationCreatedAt = destination.CreatedAt.UnixMilli()
	}
	return retryTask
}

func (h *messageHandler) shouldNackError(err error) bool {
	if err == nil {
		return false // Success case, always ack
	}

	// Handle pre-delivery errors (system errors)
	var preErr *PreDeliveryError
	if errors.As(err, &preErr) {
		// Don't nack if it's a permanent error
		if isPermanentPreDeliveryErr(preErr.err) {
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

func (h *messageHandler) scheduleRetry(ctx context.Context, task models.DeliveryTask, destination *models.Destination) (time.Duration, error) {
	// Attempt is 1-indexed; backoff schedule is 0-indexed.
	// Clamp to 0 to safely handle any leftover Attempt=0 in-flight tasks.
	backoffDuration := h.retryPolicyFor(destination.Type).backoff.Duration(max(task.Attempt-1, 0))

	retryTask := h.retryTaskFor(task, destination)
	retryTaskStr, err := retryTask.ToString()
	if err != nil {
		return backoffDuration, err
	}

	if err := h.retryScheduler.Schedule(ctx, retryTaskStr, backoffDuration, scheduler.WithTaskID(models.RetryID(task.Event.ID, task.DestinationID))); err != nil {
		h.logger.Ctx(ctx).Error("failed to schedule retry",
			zap.Error(err),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", task.DestinationID),
			zap.Int("attempt", task.Attempt),
			zap.Duration("backoff", backoffDuration))
		return backoffDuration, err
	}

	return backoffDuration, nil
}

// ensurePublishableDestination ensures that the destination exists and is in a publishable state.
// Returns an error if the destination is not found, deleted, from another generation, expired,
// disabled, or any other state that would prevent publishing. An automatic retry of a parked
// type is parked on its disabled destination instead of being dropped.
func (h *messageHandler) ensurePublishableDestination(ctx context.Context, task models.DeliveryTask) (*models.Destination, error) {
	destination, err := h.retrieveDestination(ctx, task)
	if err != nil {
		return nil, err
	}
	err = h.checkPublishable(ctx, task, destination)
	if err == nil {
		return destination, nil
	}
	if !errors.Is(err, errDestinationDisabled) || !h.shouldPark(task, destination) {
		return nil, err
	}

	logger := h.logger.Ctx(ctx)
	parked, err := h.retryParker.ParkRetry(ctx, h.retryTaskFor(task, destination), destination)
	if err != nil {
		logger.Error("failed to park retry",
			zap.Error(err),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type),
			zap.Int("attempt", task.Attempt))
		return nil, fmt.Errorf("failed to park retry: %w", err)
	}
	if parked {
		logger.Debug("retry parked on disabled destination",
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type),
			zap.Int("attempt", task.Attempt))
		return nil, errRetryParked
	}

	// The store saw the destination enabled again: it was re-enabled after our
	// read. Re-read so the attempt runs against the current state (a refresh
	// may also have rotated credentials or moved the expiry).
	destination, err = h.retrieveDestination(ctx, task)
	if err != nil {
		return nil, err
	}
	if err := h.checkPublishable(ctx, task, destination); err != nil {
		if errors.Is(err, errDestinationDisabled) {
			return nil, errDestinationDisabledAgain
		}
		return nil, err
	}
	return destination, nil
}

// shouldPark reports whether a task whose destination is disabled should be
// parked rather than dropped: automatic retries of parked types only. First
// attempts belong to events that matched while the destination was enabled
// and are dropped as before; manual retries are user-initiated and never
// deferred.
func (h *messageHandler) shouldPark(task models.DeliveryTask, destination *models.Destination) bool {
	return h.retryParker != nil &&
		h.parkedRetryTypes[destination.Type] &&
		task.Attempt > 1 &&
		!task.Manual
}

// checkPublishable checks a retrieved destination, in order: retry generation,
// expiry, disabled. Stale and expired tasks are dropped before the disabled
// check so they are never parked.
func (h *messageHandler) checkPublishable(ctx context.Context, task models.DeliveryTask, destination *models.Destination) error {
	if task.DestinationCreatedAt != 0 && task.DestinationCreatedAt != destination.CreatedAt.UnixMilli() {
		h.logger.Ctx(ctx).Debug("dropping retry for an earlier destination generation",
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type),
			zap.Int64("task_destination_created_at", task.DestinationCreatedAt),
			zap.Int64("destination_created_at", destination.CreatedAt.UnixMilli()))
		return errDestinationGenerationMismatch
	}
	if h.isExpired(destination, h.now()) {
		h.logger.Ctx(ctx).Debug("skipping expired destination",
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type))
		return errDestinationExpired
	}
	if destination.DisabledAt != nil {
		h.logger.Ctx(ctx).Debug("skipping disabled destination",
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", destination.ID),
			zap.String("destination_type", destination.Type),
			zap.Time("disabled_at", *destination.DisabledAt))
		return errDestinationDisabled
	}
	return nil
}

func (h *messageHandler) retrieveDestination(ctx context.Context, task models.DeliveryTask) (*models.Destination, error) {
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
			logger.Debug("destination deleted", fields...)
		} else {
			// Unexpected errors like DB connection issues
			logger.Error("failed to retrieve destination", fields...)
		}
		return nil, err
	}
	if destination == nil {
		h.logger.Ctx(ctx).Debug("destination not found",
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", task.DestinationID))
		return nil, tenantstore.ErrDestinationNotFound
	}
	return destination, nil
}
