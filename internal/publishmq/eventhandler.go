package publishmq

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/emetrics"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/topicschema"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

var (
	ErrInvalidTopic  = errors.New("invalid topic")
	ErrRequiredTopic = errors.New("topic is required")
	ErrInvalidData   = errors.New("data must be a valid JSON object")
	// ErrSchemaValidation matches every *SchemaValidationError.
	ErrSchemaValidation = errors.New("event data does not match the topic schema")
)

// SchemaValidationError rejects an event whose data fails its topic's payload
// schema in enforce mode. Errors never contain payload values, so they are
// safe to log and to return to the publisher.
type SchemaValidationError struct {
	Topic  string
	Errors []string
}

func (e *SchemaValidationError) Error() string {
	return fmt.Sprintf("%s: topic %q: %s", ErrSchemaValidation, e.Topic, strings.Join(e.Errors, "; "))
}

func (e *SchemaValidationError) Is(target error) bool {
	return target == ErrSchemaValidation
}

// SchemaValidator validates event data against the topic's payload schema.
// *topicschema.Catalog implements it.
type SchemaValidator interface {
	ValidateData(topic string, data []byte) topicschema.ValidationResult
}

// validationLimiter is implemented by schema validators that parse data only
// for some topics and up to a size, such as *topicschema.Catalog.
type validationLimiter interface {
	MaxValidationBytes() int
	// ValidationFactor weighs a validation of the topic's data, as a
	// multiple of its size. It is 0 when the topic's data is never parsed.
	ValidationFactor(topic string) int
}

// maxConcurrentEnqueues bounds the delivery tasks one publish enqueues at
// once.
const maxConcurrentEnqueues = 32

// validationBudgetFactor bounds the validations running at once to this many
// times the validator's size limit, each weighing its data size times its
// topic's ValidationFactor. Validating takes up to a few hundred times the
// data size in memory, so concurrent publishes must not all validate at once.
const validationBudgetFactor = 4

type EventHandler interface {
	Handle(ctx context.Context, event *models.Event) (*HandleResult, error)
}

type HandleResult struct {
	EventID   string `json:"id"`
	Duplicate bool   `json:"duplicate"`
	// DestinationIDs lists every matched destination, MCP subscriptions
	// included (unlike Event.MatchedDestinationIDs).
	DestinationIDs []string `json:"destination_ids"`
	// MatchedDestinationTypes holds the type of each DestinationIDs entry, at
	// the same index, so API versions can hide types they don't expose.
	MatchedDestinationTypes []string `json:"-"`
}

type eventHandler struct {
	emeter               emetrics.OutpostMetrics
	eventTracer          eventtracer.EventTracer
	logger               *logging.Logger
	idempotence          idempotence.Idempotence
	deliveryMQ           *deliverymq.DeliveryMQ
	tenantStore          tenantstore.TenantStore
	topics               []string
	topicsAllowWildcards bool
	schemaValidator      SchemaValidator
	// validating bounds the weight of the validations running at once to
	// validationBudget when the validator is a validationLimiter, limiter.
	// It is nil otherwise.
	validating       *semaphore.Weighted
	validationBudget int64
	limiter          validationLimiter
	// mcpTopicEnabled, when set, reports whether a topic is MCP-enabled in
	// this instance's configuration; MCP matches of other topics are dropped.
	mcpTopicEnabled func(topic string) bool
}

// EventHandlerOption configures NewEventHandler.
type EventHandlerOption func(*eventHandler)

// WithSchemaValidator validates event data at publish, as configured per
// topic. Without it no event is validated and SchemaValid stays nil. When the
// validator is a validationLimiter, publishes whose data it parses wait for
// their turn once validations weighing validationBudgetFactor times its size
// limit (MaxValidationBytes) are running. Other publishes never wait.
func WithSchemaValidator(v SchemaValidator) EventHandlerOption {
	return func(h *eventHandler) {
		h.schemaValidator = v
		h.validating, h.validationBudget, h.limiter = nil, 0, nil
		if l, ok := v.(validationLimiter); ok && l.MaxValidationBytes() > 0 {
			h.limiter = l
			h.validationBudget = math.MaxInt64
			if n := int64(l.MaxValidationBytes()); n <= h.validationBudget/validationBudgetFactor {
				h.validationBudget = n * validationBudgetFactor
			}
			h.validating = semaphore.NewWeighted(h.validationBudget)
		}
	}
}

// WithMCPTopicCheck drops matched MCP subscriptions (type mcp) when
// enabled(event topic) is false, so a topic that is no longer MCP-enabled
// stops reaching its subscriptions before they are ended. Dropped
// subscriptions are neither enqueued nor listed in the result.
func WithMCPTopicCheck(enabled func(topic string) bool) EventHandlerOption {
	return func(h *eventHandler) {
		h.mcpTopicEnabled = enabled
	}
}

// WithMetrics replaces the default OpenTelemetry metrics.
func WithMetrics(m emetrics.OutpostMetrics) EventHandlerOption {
	return func(h *eventHandler) {
		h.emeter = m
	}
}

func NewEventHandler(
	logger *logging.Logger,
	deliveryMQ *deliverymq.DeliveryMQ,
	tenantStore tenantstore.TenantStore,
	eventTracer eventtracer.EventTracer,
	topics []string,
	topicsAllowWildcards bool,
	idempotence idempotence.Idempotence,
	opts ...EventHandlerOption,
) EventHandler {
	eventHandler := &eventHandler{
		logger:               logger,
		idempotence:          idempotence,
		deliveryMQ:           deliveryMQ,
		tenantStore:          tenantStore,
		eventTracer:          eventTracer,
		topics:               topics,
		topicsAllowWildcards: topicsAllowWildcards,
	}
	for _, opt := range opts {
		opt(eventHandler)
	}
	if eventHandler.emeter == nil {
		eventHandler.emeter, _ = emetrics.New()
	}
	return eventHandler
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, event *models.Event) (*HandleResult, error) {
	if len(h.topics) > 0 && event.Topic == "" {
		return nil, ErrRequiredTopic
	}
	if len(h.topics) > 0 && event.Topic != "*" && !slices.Contains(h.topics, event.Topic) {
		return nil, ErrInvalidTopic
	}

	logger := h.logger.Ctx(ctx)
	receivedAt := time.Now()

	// Wide event state: accumulated through Handle, emitted as a single
	// event.received line by the defer below.
	var enqueuedMu sync.Mutex
	var enqueued []string
	var matched []tenantstore.MatchedDestination
	var duplicate bool
	var enqueueFailed bool
	var matchFailed bool
	var mcpDropped int
	var schema schemaOutcome

	defer func() {
		enqueuedMu.Lock()
		enqueuedCopy := append([]string{}, enqueued...)
		enqueuedMu.Unlock()

		matchedIDs := make([]string, len(matched))
		for i, m := range matched {
			matchedIDs[i] = m.ID
		}
		fields := []zap.Field{
			zap.String("event_id", event.ID),
			zap.String("tenant_id", event.TenantID),
			zap.String("topic", event.Topic),
			zap.Int("matched_destination_count", len(matched)),
			zap.Strings("matched_destination_ids", matchedIDs),
			zap.Int("enqueued_destination_count", len(enqueuedCopy)),
			zap.Strings("enqueued_destination_ids", enqueuedCopy),
			zap.Bool("duplicate", duplicate),
			zap.Time("event_received_at", receivedAt),
			zap.Int64("duration_ms", time.Since(receivedAt).Milliseconds()),
		}
		if event.DestinationID != "" {
			fields = append(fields, zap.String("destination_id", event.DestinationID))
		}
		if matchFailed {
			fields = append(fields, zap.Bool("match_failed", true))
		}
		if mcpDropped > 0 {
			fields = append(fields, zap.Int("mcp_topic_disabled_count", mcpDropped))
		}
		if enqueueFailed {
			fields = append(fields, zap.Bool("enqueue_failed", true))
		}
		if schema.status != "" {
			fields = append(fields, zap.String("schema_validation", schema.status))
		}
		if len(schema.errors) > 0 {
			fields = append(fields, zap.Strings("schema_errors", schema.errors))
		}
		logger.Info("event.received", fields...)
	}()

	// Validate before matching and idempotency: a rejected publish costs no
	// store lookups and leaves its event ID unclaimed, so the publisher can
	// retry it once the data is fixed.
	var err error
	schema, err = h.validateSchema(ctx, event)
	if err != nil {
		return nil, err
	}

	// Branch: specific destination vs topic-based matching
	if event.DestinationID != "" {
		matched, err = h.matchSpecificDestination(ctx, event)
		if err != nil {
			return nil, err
		}
	} else {
		matched, err = h.tenantStore.MatchEvent(ctx, *event, h.topicsAllowWildcards)
		if err != nil {
			matchFailed = true
			logger.Error("failed to match event destinations",
				zap.Error(err),
				zap.String("event_id", event.ID),
				zap.String("tenant_id", event.TenantID))
			return nil, err
		}
	}
	matched, mcpDropped = h.dropMCPOfDisabledTopic(event.Topic, matched)

	ids := make([]string, len(matched))
	types := make([]string, len(matched))
	// Stamp matched destinations onto the event for downstream persistence.
	// MCP subscriptions are left out: the event is copied into every delivery
	// task, so carrying a tenant's (many) subscriptions would grow the
	// publish quadratically. Their deliveries show up as attempts.
	stamped := make([]string, 0, len(matched))
	for i, m := range matched {
		ids[i] = m.ID
		types[i] = m.Type
		if m.Type != models.DestinationTypeMCP {
			stamped = append(stamped, m.ID)
		}
	}
	event.MatchedDestinationIDs = stamped

	result := &HandleResult{
		EventID:                 event.ID,
		Duplicate:               false,
		DestinationIDs:          ids,
		MatchedDestinationTypes: types,
	}

	if len(matched) == 0 {
		return result, nil
	}

	// Publish deliveries (INSIDE idempotency)
	executed := false
	err = h.idempotence.Exec(ctx, idempotencyKeyFromEvent(event), func(ctx context.Context) error {
		executed = true
		return h.doPublish(ctx, event, ids, &enqueuedMu, &enqueued)
	})

	if err != nil {
		enqueueFailed = true
		return nil, err
	}

	if !executed {
		duplicate = true
		result.Duplicate = true
	}

	return result, nil
}

// Values of the schema_validation field on event.received.
const (
	schemaStatusValid    = "valid"
	schemaStatusInvalid  = "invalid"
	schemaStatusRejected = "rejected"
	schemaStatusTooLarge = "skipped_too_large"
)

// schemaOutcome is the schema check result reported on event.received.
type schemaOutcome struct {
	status string // empty when the topic is not validated
	errors []string
}

// validateSchema checks the event data against its topic's payload schema and
// stamps event.SchemaValid. That must happen before doPublish, which copies
// the event into every delivery task. Data that fails an enforced schema
// returns a *SchemaValidationError.
func (h *eventHandler) validateSchema(ctx context.Context, event *models.Event) (schemaOutcome, error) {
	// The verdict is Outpost's own: never keep a value set by the caller.
	event.SchemaValid = nil
	if h.schemaValidator == nil {
		return schemaOutcome{}, nil
	}

	if weight := h.validationWeight(event); weight > 0 {
		if err := h.validating.Acquire(ctx, weight); err != nil {
			return schemaOutcome{}, err
		}
		defer h.validating.Release(weight)
	}
	result := h.schemaValidator.ValidateData(event.Topic, event.Data)
	switch {
	case result.SkippedTooLarge:
		h.emeter.EventSchemaInvalid(ctx, event.Topic, string(result.Mode), emetrics.SchemaInvalidReasonTooLarge)
		return schemaOutcome{status: schemaStatusTooLarge}, nil
	case !result.Checked:
		return schemaOutcome{}, nil
	case result.Valid:
		valid := true
		event.SchemaValid = &valid
		return schemaOutcome{status: schemaStatusValid}, nil
	}

	reason := emetrics.SchemaInvalidReasonInvalid
	if len(result.Errors) == 1 && result.Errors[0] == topicschema.DataTooLargeError {
		reason = emetrics.SchemaInvalidReasonTooLarge
	}
	h.emeter.EventSchemaInvalid(ctx, event.Topic, string(result.Mode), reason)

	if result.Mode == topicschema.ValidationEnforce {
		return schemaOutcome{status: schemaStatusRejected, errors: result.Errors},
			&SchemaValidationError{Topic: event.Topic, Errors: result.Errors}
	}
	valid := false
	event.SchemaValid = &valid
	return schemaOutcome{status: schemaStatusInvalid, errors: result.Errors}, nil
}

// validationWeight returns what validating the event's data takes from the
// budget: its size times its topic's ValidationFactor, at most the whole
// budget. It is 0, and the publish doesn't wait, when the data isn't parsed:
// its topic isn't validated, or it's over the size limit and rejected or
// skipped unparsed.
func (h *eventHandler) validationWeight(event *models.Event) int64 {
	if h.validating == nil || len(event.Data) > h.limiter.MaxValidationBytes() {
		return 0
	}
	factor := int64(h.limiter.ValidationFactor(event.Topic))
	if factor <= 0 {
		return 0
	}
	if size := int64(len(event.Data)); size <= h.validationBudget/factor {
		return size * factor
	}
	return h.validationBudget
}

func (h *eventHandler) doPublish(ctx context.Context, event *models.Event, matchedDestinations []string, enqueuedMu *sync.Mutex, enqueued *[]string) error {
	_, span := h.eventTracer.Receive(ctx, event)
	defer span.End()

	h.emeter.EventEligbible(ctx, event)

	// Bound the fan-out: a tenant can have hundreds of matching
	// destinations (MCP subscriptions), and each enqueue holds a broker
	// round trip.
	var g errgroup.Group
	g.SetLimit(maxConcurrentEnqueues)
	for _, destID := range matchedDestinations {
		g.Go(func() error {
			if err := h.enqueueDeliveryTask(ctx, models.NewDeliveryTask(*event, destID)); err != nil {
				return err
			}
			enqueuedMu.Lock()
			*enqueued = append(*enqueued, destID)
			enqueuedMu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// dropMCPOfDisabledTopic removes the MCP subscriptions from matched when the
// topic is not MCP-enabled here (WithMCPTopicCheck), returning how many it
// removed. MCP subscriptions only match their exact topic, so the event topic
// is theirs.
func (h *eventHandler) dropMCPOfDisabledTopic(topic string, matched []tenantstore.MatchedDestination) ([]tenantstore.MatchedDestination, int) {
	if h.mcpTopicEnabled == nil || !slices.ContainsFunc(matched, isMCPMatch) || h.mcpTopicEnabled(topic) {
		return matched, 0
	}
	kept := slices.DeleteFunc(slices.Clone(matched), isMCPMatch)
	return kept, len(matched) - len(kept)
}

func isMCPMatch(m tenantstore.MatchedDestination) bool {
	return m.Type == models.DestinationTypeMCP
}

// matchSpecificDestination handles the case where a specific destination_id is provided.
// It retrieves the destination and validates it, returning the matched destination.
func (h *eventHandler) matchSpecificDestination(ctx context.Context, event *models.Event) ([]tenantstore.MatchedDestination, error) {
	destination, err := h.tenantStore.RetrieveDestination(ctx, event.TenantID, event.DestinationID)
	if err != nil {
		h.logger.Ctx(ctx).Warn("failed to retrieve destination",
			zap.Error(err),
			zap.String("event_id", event.ID),
			zap.String("tenant_id", event.TenantID),
			zap.String("destination_id", event.DestinationID))
		return nil, nil
	}

	if destination == nil {
		return nil, nil
	}

	if !destination.MatchEvent(*event, h.topicsAllowWildcards) {
		return nil, nil
	}

	return []tenantstore.MatchedDestination{{ID: destination.ID, Type: destination.Type}}, nil
}

func (h *eventHandler) enqueueDeliveryTask(ctx context.Context, task models.DeliveryTask) error {
	_, deliverySpan := h.eventTracer.StartDelivery(ctx, &task)
	defer deliverySpan.End()
	if err := h.deliveryMQ.Publish(ctx, task); err != nil {
		h.logger.Ctx(ctx).Error("failed to enqueue delivery task",
			zap.Error(err),
			zap.String("event_id", task.Event.ID),
			zap.String("tenant_id", task.Event.TenantID),
			zap.String("destination_id", task.DestinationID))
		deliverySpan.RecordError(err)
		return err
	}
	return nil
}

func idempotencyKeyFromEvent(event *models.Event) string {
	return "idempotency:publishmq:" + event.ID
}
