package logmq

import (
	"context"

	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mqs"
)

type batcher interface {
	Add(ctx context.Context, msg *mqs.Message) error
}

type messageHandler struct {
	logger  *logging.Logger
	batcher batcher
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func NewMessageHandler(logger *logging.Logger, batcher batcher) consumer.MessageHandler {
	return &messageHandler{
		logger:  logger,
		batcher: batcher,
	}
}

func (h *messageHandler) Handle(ctx context.Context, msg *mqs.Message) error {
	logger := h.logger.Ctx(ctx)
	logger.Info("logmq handler")
	h.batcher.Add(ctx, msg)
	return nil
}
