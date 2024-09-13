package delivery

import (
	"context"

	"github.com/hookdeck/EventKit/internal/deliverer"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

type EventHandler interface {
	Handle(ctx context.Context, event ingest.Event) error
}

type eventHandler struct {
	logger           *otelzap.Logger
	redisClient      *redis.Client
	destinationModel *models.DestinationModel
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, event ingest.Event) error {
	destinations, err := h.destinationModel.List(ctx, h.redisClient, event.TenantID)
	if err != nil {
		return err
	}
	destinations = models.FilterTopics(destinations, event.Topic)

	// TODO: handle via goroutine or MQ.
	for _, destination := range destinations {
		deliverer.New(h.logger).Deliver(ctx, &destination, &event)
	}

	return nil
}
