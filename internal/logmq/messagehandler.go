package logmq

import (
	"context"

	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

type batcher interface {
	Add(ctx context.Context, msg *mqs.Message) error
}

type messageHandler struct {
	logger  *otelzap.Logger
	batcher batcher
}

var _ consumer.MessageHandler = (*messageHandler)(nil)

func NewMessageHandler(logger *otelzap.Logger, batcher batcher) consumer.MessageHandler {
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
