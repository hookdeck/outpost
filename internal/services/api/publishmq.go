package api

import (
	"context"
	"errors"

	"github.com/hookdeck/EventKit/internal/consumer"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/publishmq"
	"go.uber.org/zap"
)

func (s *APIService) SubscribePublishMQ(ctx context.Context, subscription mqs.Subscription, eventHandler publishmq.EventHandler, concurrency int) {
	messageHandler := publishmq.NewMessageHandler(
		eventHandler,
	)
	csm := consumer.New(subscription, messageHandler,
		consumer.WithConcurrency(concurrency),
		consumer.WithName("publishmq"),
	)
	if err := csm.Run(ctx); !errors.Is(err, ctx.Err()) {
		s.logger.Ctx(ctx).Error("failed to run publishmq consumer", zap.Error(err))
	}
}
