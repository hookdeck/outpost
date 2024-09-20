package api

import (
	"context"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/mqs"
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
		event := models.Event{}
		err = event.FromMessage(msg)
		if err != nil {
			logger.Info("error parsing message", zap.Error(err))
			msg.Ack()
			continue
		}
		logger.Info("received event", zap.Any("event", event))
		if event.ID == "" {
			event.ID = uuid.New().String()
		}

		err = s.deliveryMQ.Publish(ctx, event)
		if err != nil {
			logger.Info("error publishing message to deliverymq", zap.Error(err))
			msg.Nack()
			continue
		}
		msg.Ack()
	}
}
