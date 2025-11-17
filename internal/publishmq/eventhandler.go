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
	EventID           string                  `json:"id"`
	Duplicate         bool                    `json:"duplicate"`
	DestinationStatus *DestinationMatchStatus `json:"destination_status,omitempty"`
}

type DestinationMatchStatus string

const (
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
		zap.String("topic", event.Topic),
		zap.String("destination_id", event.DestinationID))

	var matchedDestinations []models.DestinationSummary
	var destStatus *DestinationMatchStatus
	var err error

	// Branch: specific destination vs topic-based matching
	if event.DestinationID != "" {
		// Specific destination path
		matchedDestinations, destStatus, err = h.matchSpecificDestination(ctx, event)
		if err != nil {
			return nil, err
		}
	} else {
		// Topic-based matching path
		matchedDestinations, err = h.entityStore.MatchEvent(ctx, *event)
		if err != nil {
			logger.Error("failed to match event destinations",
				zap.Error(err),
				zap.String("event_id", event.ID),
				zap.String("tenant_id", event.TenantID))
			return nil, err
		}
	}

	result := &HandleResult{
		EventID:   event.ID,
		Duplicate: false,
	}

	// Early return if no destinations matched
	if len(matchedDestinations) == 0 {
		// Only set destination_status if destination_id was specified and nothing matched
		if event.DestinationID != "" && destStatus != nil {
			result.DestinationStatus = destStatus
		}
		logger.Info("no matching destinations",
			zap.String("event_id", event.ID),
			zap.String("tenant_id", event.TenantID))
		return result, nil
	}

	// Publish deliveries (INSIDE idempotency)
	executed := false
	err = h.idempotence.Exec(ctx, idempotencyKeyFromEvent(event), func(ctx context.Context) error {
		executed = true
		return h.doPublish(ctx, event, matchedDestinations)
	})

	if err != nil {
		return nil, err
	}

	// Set duplicate flag if not executed (idempotency hit)
	if !executed {
		result.Duplicate = true
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

// matchSpecificDestination handles the case where a specific destination_id is provided.
// It retrieves the destination and validates it, returning both the matched destinations
// and the status (only set when nothing matched - disabled/not_found/topic_mismatch).
func (h *eventHandler) matchSpecificDestination(ctx context.Context, event *models.Event) ([]models.DestinationSummary, *DestinationMatchStatus, error) {
	destination, err := h.entityStore.RetrieveDestination(ctx, event.TenantID, event.DestinationID)
	if err != nil {
		h.logger.Ctx(ctx).Warn("failed to retrieve destination",
			zap.Error(err),
			zap.String("destination_id", event.DestinationID))
		status := DestinationStatusNotFound
		return []models.DestinationSummary{}, &status, nil
	}

	if destination == nil {
		status := DestinationStatusNotFound
		return []models.DestinationSummary{}, &status, nil
	}

	if destination.DisabledAt != nil {
		status := DestinationStatusDisabled
		return []models.DestinationSummary{}, &status, nil
	}

	// Check topic match
	if event.Topic != "" && event.Topic != "*" && destination.Topics[0] != "*" && !slices.Contains(destination.Topics, event.Topic) {
		status := DestinationStatusTopicMismatch
		return []models.DestinationSummary{}, &status, nil
	}

	// Matched! Return nil status since it will be queued
	return []models.DestinationSummary{*destination.ToSummary()}, nil, nil
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
