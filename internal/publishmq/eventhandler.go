package publishmq

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/emetrics"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var (
	ErrInvalidTopic  = errors.New("invalid topic")
	ErrRequiredTopic = errors.New("topic is required")
	ErrInvalidData   = errors.New("data must be a valid JSON object")
)

type EventHandler interface {
	Handle(ctx context.Context, event *models.Event) (*HandleResult, error)
}

type HandleResult struct {
	EventID        string   `json:"id"`
	Duplicate      bool     `json:"duplicate"`
	DestinationIDs []string `json:"destination_ids"`
}

type eventHandler struct {
	emeter      emetrics.OutpostMetrics
	eventTracer eventtracer.EventTracer
	logger      *logging.Logger
	idempotence idempotence.Idempotence
	deliveryMQ  *deliverymq.DeliveryMQ
	tenantStore tenantstore.TenantStore
	topics      []string
}

func NewEventHandler(
	logger *logging.Logger,
	deliveryMQ *deliverymq.DeliveryMQ,
	tenantStore tenantstore.TenantStore,
	eventTracer eventtracer.EventTracer,
	topics []string,
	idempotence idempotence.Idempotence,
) EventHandler {
	emeter, _ := emetrics.New()
	eventHandler := &eventHandler{
		logger:      logger,
		idempotence: idempotence,
		deliveryMQ:  deliveryMQ,
		tenantStore: tenantStore,
		eventTracer: eventTracer,
		topics:      topics,
		emeter:      emeter,
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

	// Wide event state: populated by the rest of Handle and emitted as a single
	// audit at the end. Replaces the separate "processing event" and per-
	// destination "delivery task enqueued" audits.
	var enqueuedMu sync.Mutex
	var enqueued []string
	var matched []string
	var duplicate bool
	var enqueueFailed bool
	var matchFailed bool

	defer func() {
		enqueuedMu.Lock()
		enqueuedCopy := append([]string{}, enqueued...)
		enqueuedMu.Unlock()

		fields := []zap.Field{
			zap.String("event_id", event.ID),
			zap.String("tenant_id", event.TenantID),
			zap.String("topic", event.Topic),
			zap.Int("matched_destination_count", len(matched)),
			zap.Strings("matched_destination_ids", matched),
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
		if enqueueFailed {
			fields = append(fields, zap.Bool("enqueue_failed", true))
		}
		logger.Info("event.received", fields...)
	}()

	var err error

	// Branch: specific destination vs topic-based matching
	if event.DestinationID != "" {
		matched, err = h.matchSpecificDestination(ctx, event)
		if err != nil {
			return nil, err
		}
	} else {
		matched, err = h.tenantStore.MatchEvent(ctx, *event)
		if err != nil {
			matchFailed = true
			logger.Error("failed to match event destinations",
				zap.Error(err),
				zap.String("event_id", event.ID),
				zap.String("tenant_id", event.TenantID))
			return nil, err
		}
	}

	if matched == nil {
		matched = []string{}
	}

	// Stamp matched destinations onto the event for downstream persistence.
	event.MatchedDestinationIDs = matched

	result := &HandleResult{
		EventID:        event.ID,
		Duplicate:      false,
		DestinationIDs: matched,
	}

	if len(matched) == 0 {
		return result, nil
	}

	// Publish deliveries (INSIDE idempotency)
	executed := false
	err = h.idempotence.Exec(ctx, idempotencyKeyFromEvent(event), func(ctx context.Context) error {
		executed = true
		return h.doPublish(ctx, event, matched, &enqueuedMu, &enqueued)
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

func (h *eventHandler) doPublish(ctx context.Context, event *models.Event, matchedDestinations []string, enqueuedMu *sync.Mutex, enqueued *[]string) error {
	_, span := h.eventTracer.Receive(ctx, event)
	defer span.End()

	h.emeter.EventEligbible(ctx, event)

	var g errgroup.Group
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

// matchSpecificDestination handles the case where a specific destination_id is provided.
// It retrieves the destination and validates it, returning the matched destination IDs.
func (h *eventHandler) matchSpecificDestination(ctx context.Context, event *models.Event) ([]string, error) {
	destination, err := h.tenantStore.RetrieveDestination(ctx, event.TenantID, event.DestinationID)
	if err != nil {
		h.logger.Ctx(ctx).Warn("failed to retrieve destination",
			zap.Error(err),
			zap.String("event_id", event.ID),
			zap.String("tenant_id", event.TenantID),
			zap.String("destination_id", event.DestinationID))
		return []string{}, nil
	}

	if destination == nil {
		return []string{}, nil
	}

	if !destination.MatchEvent(*event) {
		return []string{}, nil
	}

	return []string{destination.ID}, nil
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
