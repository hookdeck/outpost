package logmq

import (
	"context"

	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mqs"
	"go.uber.org/zap"
)

// BatchAdder is the interface for adding messages to a batch processor.
type BatchAdder interface {
	Add(ctx context.Context, msg *mqs.Message) error
}

type messageHandler struct {
	logger     *logging.Logger
	batchAdder BatchAdder
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func NewMessageHandler(logger *logging.Logger, batchAdder BatchAdder) consumer.MessageHandler {
	return &messageHandler{
		logger:     logger,
		batchAdder: batchAdder,
	}
}

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	logger := h.logger.Ctx(ctx)
	logger.Info("logmq handler",
		zap.String("message_id", msg.LoggableID))
	h.batchAdder.Add(ctx, msg)
	return nil
}
