package publishmq

import (
	"context"
	"errors"
	"time"

	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

const (
	VisibilityTimeout = 5            // 5 seconds
	SuccessfulTTL     = 60 * 60 * 24 // 24 hours
	StatusProcessing  = "processing"
	StatusProcessed   = "processed"
)

var ErrConflict = errors.New("conflict")

type EventHandler interface {
	Handle(ctx context.Context, event *models.Event) error
}

type eventHandler struct {
	logger      *otelzap.Logger
	redisClient *redis.Client
	deliveryMQ  *deliverymq.DeliveryMQ
}

func NewEventHandler(logger *otelzap.Logger, redisClient *redis.Client, deliveryMQ *deliverymq.DeliveryMQ) EventHandler {
	return &eventHandler{
		logger:      logger,
		redisClient: redisClient,
		deliveryMQ:  deliveryMQ,
	}
}

var _ EventHandler = (*eventHandler)(nil)

func (h *eventHandler) Handle(ctx context.Context, event *models.Event) error {
	// Check idempotency
	idempotencyKey := idempotencyKeyFromEvent(event)
	isIdempotent, err := h.checkIdempotency(ctx, idempotencyKey)
	if err != nil {
		h.logger.Info("error checking idempotency", zap.Error(err))
		return err
	}
	if !isIdempotent {
		processingStatus, err := h.getIdempotencyStatus(ctx, idempotencyKey)
		if err != nil {
			h.logger.Info("error getting idempotency status", zap.Error(err))
			return err
		}
		h.logger.Info("message is not idempotent", zap.String("status", processingStatus))
		if processingStatus == StatusProcessed {
			return nil
		}
		if processingStatus == StatusProcessing {
			time.Sleep((VisibilityTimeout + 1) * time.Second)
			status, err := h.getIdempotencyStatus(ctx, idempotencyKey)
			if err != nil {
				h.logger.Info("error getting idempotency status", zap.Error(err))
				return err
			}
			if status == StatusProcessed {
				return nil
			}
			return ErrConflict
		}
		return errors.New("unknown idempotency status")
	}

	// Message handling logic
	h.logger.Info("received event", zap.Any("event", event))
	err = h.deliveryMQ.Publish(ctx, *event)
	if err != nil {
		h.logger.Info("error publishing message to deliverymq", zap.Error(err))
		clearErr := h.clearIdempotency(ctx, idempotencyKey)
		if clearErr != nil {
			h.logger.Info("error clearing idempotency", zap.Error(clearErr))
			return errors.Join(err, clearErr)
		}
		return err
	}

	err = h.markProcessedIdempotency(ctx, idempotencyKey)
	if err != nil {
		h.logger.Info("error marking processed idempotency", zap.Error(err))
		return err
	}

	return nil
}

func (h *eventHandler) checkIdempotency(ctx context.Context, idempotencyKey string) (bool, error) {
	idempotentValue, err := h.redisClient.SetNX(ctx, idempotencyKey, StatusProcessing, VisibilityTimeout*time.Second).Result()
	if err != nil {
		return false, err
	}
	return idempotentValue, nil
}

func (h *eventHandler) getIdempotencyStatus(ctx context.Context, idempotencyKey string) (string, error) {
	return h.redisClient.Get(ctx, idempotencyKey).Result()
}

func (h *eventHandler) markProcessedIdempotency(ctx context.Context, idempotencyKey string) error {
	return h.redisClient.Set(ctx, idempotencyKey, StatusProcessed, SuccessfulTTL*time.Second).Err()
}

func (h *eventHandler) clearIdempotency(ctx context.Context, idempotencyKey string) error {
	return h.redisClient.Del(ctx, idempotencyKey).Err()
}

func idempotencyKeyFromEvent(event *models.Event) string {
	return "event:" + event.ID
}
