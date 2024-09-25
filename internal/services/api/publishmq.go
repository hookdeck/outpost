package api

import (
	"context"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/publishmq"
	"go.uber.org/zap"
)

func (s *APIService) SubscribePublishMQ(ctx context.Context, subscription mqs.Subscription) {
	defer subscription.Shutdown(ctx)
	for {
		msg, err := subscription.Receive(ctx)
		logger := s.logger.Ctx(ctx)
		if err != nil {
			if err == context.Canceled {
				logger.Info("context canceled")
				break
			}
			logger.Error("failed to receive message", zap.Error(err))
			return
		}
		eventHandler := publishmq.NewEventHandler(s.logger, s.redisClient, s.deliveryMQ, s.destinationModel)
		event := models.Event{}
		err = event.FromMessage(msg)
		if err != nil {
			logger.Info("error parsing message", zap.Error(err))
			msg.Ack()
			continue
		}
		if event.ID == "" {
			event.ID = uuid.New().String()
		}
		err = eventHandler.Handle(ctx, &event)
		if err != nil {
			// TODO differentiate different error cases?
			msg.Nack()
		} else {
			msg.Ack()
		}
	}
}
