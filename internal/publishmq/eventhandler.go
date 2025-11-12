package publishmq

import (
	"context"
	"errors"
	"slices"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/emetrics"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var (
	ErrInvalidTopic  = errors.New("invalid topic")
	ErrRequiredTopic = errors.New("topic is required")
)

type EventHandler interface {
	Handle(ctx context.Context, event *models.Event) (*HandleResult, error)
}

type HandleResult struct {
	EventID      string              `json:"id"`
	MatchedCount int                 `json:"matched_count"`
	QueuedCount  int                 `json:"queued_count"`
	Destinations []DestinationStatus `json:"destinations,omitempty"`
}

type DestinationStatus struct {
	ID     string                 `json:"id"`
	Status DestinationMatchStatus `json:"status"`
}

type DestinationMatchStatus string

const (
	DestinationStatusQueued        DestinationMatchStatus = "queued"
	DestinationStatusDisabled      DestinationMatchStatus = "disabled"
	DestinationStatusNotFound      DestinationMatchStatus = "not_found"
	DestinationStatusTopicMismatch DestinationMatchStatus = "topic_mismatch"
)

type eventHandler struct {
	emeter      emetrics.OutpostMetrics
	eventTracer eventtracer.EventTracer
	logger      *logging.Logger
	idempotence idempotence.Idempotence
	deliveryMQ  *deliverymq.DeliveryMQ
	entityStore models.EntityStore
	topics      []string
}

func NewEventHandler(
	logger *logging.Logger,
	deliveryMQ *deliverymq.DeliveryMQ,
	entityStore models.EntityStore,
	eventTracer eventtracer.EventTracer,
	topics []string,
	idempotence idempotence.Idempotence,
) EventHandler {
	emeter, _ := emetrics.New()
	eventHandler := &eventHandler{
		logger:      logger,
		idempotence: idempotence,
		deliveryMQ:  deliveryMQ,
		entityStore: entityStore,
		eventTracer: eventTracer,
		topics:      topics,
		emeter:      emeter,
	}
	return eventHandler
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, event *models.Event) (*HandleResult, error) {
	logger := h.logger.Ctx(ctx)

	if len(h.topics) > 0 && event.Topic == "" {
		return nil, ErrRequiredTopic
	}
	if len(h.topics) > 0 && event.Topic != "*" && !slices.Contains(h.topics, event.Topic) {
		return nil, ErrInvalidTopic
	}

	logger.Audit("processing event",
		zap.String("event_id", event.ID),
		zap.String("tenant_id", event.TenantID),
		zap.String("topic", event.Topic))

	// Step 1: Match destinations (OUTSIDE idempotency)
	matchedDestinations, err := h.entityStore.MatchEvent(ctx, *event)
	if err != nil {
		logger.Error("failed to match event destinations",
			zap.Error(err),
			zap.String("event_id", event.ID),
			zap.String("tenant_id", event.TenantID))
		return nil, err
	}

	// Step 2: Build result
	result := &HandleResult{
		EventID:      event.ID,
		MatchedCount: len(matchedDestinations),
		QueuedCount:  0,
	}

	// Add destination status if destination_id specified
	if event.DestinationID != "" {
		destStatus := h.getDestinationStatus(ctx, event, matchedDestinations)
		result.Destinations = []DestinationStatus{destStatus}
	}

	// Early return if no destinations matched
	if len(matchedDestinations) == 0 {
		logger.Info("no matching destinations",
			zap.String("event_id", event.ID),
			zap.String("tenant_id", event.TenantID))
		return result, nil
	}

	// Step 3: Publish deliveries (INSIDE idempotency)
	executed := false
	err = h.idempotence.Exec(ctx, idempotencyKeyFromEvent(event), func(ctx context.Context) error {
		executed = true
		return h.doPublish(ctx, event, matchedDestinations)
	})

	if err != nil {
		return nil, err
	}

	// Step 4: Set queued count only if actually executed
	if executed {
		result.QueuedCount = len(matchedDestinations)
	}

	return result, nil
}

func (h *eventHandler) doPublish(ctx context.Context, event *models.Event, matchedDestinations []models.DestinationSummary) error {
	_, span := h.eventTracer.Receive(ctx, event)
	defer span.End()

	h.emeter.EventEligbible(ctx, event)

	var g errgroup.Group
	for _, destinationSummary := range matchedDestinations {
		destID := destinationSummary.ID
		g.Go(func() error {
			return h.enqueueDeliveryEvent(ctx, models.NewDeliveryEvent(*event, destID))
		})
	}
	if err := g.Wait(); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// getDestinationStatus determines the status of a specific destination.
// This is only called when event.DestinationID is specified in the request.
// Returns one of: queued, disabled, not_found, topic_mismatch
func (h *eventHandler) getDestinationStatus(ctx context.Context, event *models.Event, matchedDestinations []models.DestinationSummary) DestinationStatus {
	// Check if destination was matched
	for _, dest := range matchedDestinations {
		if dest.ID == event.DestinationID {
			return DestinationStatus{
				ID:     event.DestinationID,
				Status: DestinationStatusQueued,
			}
		}
	}

	// Destination not matched - determine why
	destination, err := h.entityStore.RetrieveDestination(ctx, event.TenantID, event.DestinationID)
	if err != nil {
		h.logger.Ctx(ctx).Warn("failed to retrieve destination for status check",
			zap.Error(err),
			zap.String("destination_id", event.DestinationID))
		return DestinationStatus{
			ID:     event.DestinationID,
			Status: DestinationStatusNotFound,
		}
	}

	if destination == nil {
		return DestinationStatus{
			ID:     event.DestinationID,
			Status: DestinationStatusNotFound,
		}
	}

	if destination.DisabledAt != nil {
		return DestinationStatus{
			ID:     event.DestinationID,
			Status: DestinationStatusDisabled,
		}
	}

	return DestinationStatus{
		ID:     event.DestinationID,
		Status: DestinationStatusTopicMismatch,
	}
}

func (h *eventHandler) enqueueDeliveryEvent(ctx context.Context, deliveryEvent models.DeliveryEvent) error {
	_, deliverySpan := h.eventTracer.StartDelivery(ctx, &deliveryEvent)
	if err := h.deliveryMQ.Publish(ctx, deliveryEvent); err != nil {
		h.logger.Ctx(ctx).Error("failed to enqueue delivery event",
			zap.Error(err),
			zap.String("delivery_event_id", deliveryEvent.ID),
			zap.String("event_id", deliveryEvent.Event.ID),
			zap.String("destination_id", deliveryEvent.DestinationID))
		deliverySpan.RecordError(err)
		deliverySpan.End()
		return err
	}

	h.logger.Ctx(ctx).Audit("delivery event enqueued",
		zap.String("delivery_event_id", deliveryEvent.ID),
		zap.String("event_id", deliveryEvent.Event.ID),
		zap.String("destination_id", deliveryEvent.DestinationID))
	deliverySpan.End()
	return nil
}

func idempotencyKeyFromEvent(event *models.Event) string {
	return "idempotency:publishmq:" + event.ID
}
