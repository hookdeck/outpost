package publishmq

import (
	"context"
	"time"

	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/idempotence"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type EventHandler interface {
	Handle(ctx context.Context, event *models.Event) error
}

type eventHandler struct {
	logger      *otelzap.Logger
	idempotence idempotence.Idempotence
	deliveryMQ  *deliverymq.DeliveryMQ
}

func NewEventHandler(logger *otelzap.Logger, redisClient *redis.Client, deliveryMQ *deliverymq.DeliveryMQ) EventHandler {
	return &eventHandler{
		logger: logger,
		idempotence: idempotence.New(redisClient,
			idempotence.WithTimeout(5*time.Second),
			idempotence.WithSuccessfulTTL(24*time.Hour),
		),
		deliveryMQ: deliveryMQ,
	}
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, event *models.Event) error {
	return h.idempotence.Exec(ctx, idempotencyKeyFromEvent(event), func() error {
		// Message handling logic
		h.logger.Info("received event", zap.Any("event", event))
		return h.deliveryMQ.Publish(ctx, *event)
	})
}

func idempotencyKeyFromEvent(event *models.Event) string {
	return "event:" + event.ID
}
