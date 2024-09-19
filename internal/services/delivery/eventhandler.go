package delivery

import (
	"context"

	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

type EventHandler interface {
	Handle(ctx context.Context, event models.Event) error
}

type eventHandler struct {
	logger           *otelzap.Logger
	redisClient      *redis.Client
	destinationModel *models.DestinationModel
}

func NewEventHandler(logger *otelzap.Logger, redisClient *redis.Client, destinationModel *models.DestinationModel) EventHandler {
	return &eventHandler{
		logger:           logger,
		redisClient:      redisClient,
		destinationModel: destinationModel,
	}
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, event models.Event) error {
	destinations, err := h.destinationModel.List(ctx, h.redisClient, event.TenantID)
	if err != nil {
		return err
	}
	destinations = models.FilterTopics(destinations, event.Topic)

	// TODO: handle via goroutine or MQ.
	for _, destination := range destinations {
		destination.Publish(ctx, &event)
	}

	return nil
}
