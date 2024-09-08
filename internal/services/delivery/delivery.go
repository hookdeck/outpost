package delivery

import (
	"context"
	"fmt"
	"sync"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	_ "gocloud.dev/pubsub/mempubsub"
)

type DeliveryService struct {
	logger *otelzap.Logger
}

func NewService(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *otelzap.Logger) (*DeliveryService, error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		logger.Ctx(ctx).Info("service shutdown", zap.String("service", "delivery"))
	}()

	service := &DeliveryService{
		logger: logger,
	}

	return service, nil
}

func (s *DeliveryService) Run(ctx context.Context) error {
	s.logger.Ctx(ctx).Info("start service", zap.String("service", "delivery"))

	ingestor := ingest.New(s.logger, nil)
	closeDeliveryTopic, err := ingestor.OpenDeliveryTopic(ctx)
	defer closeDeliveryTopic()

	subscription, err := ingestor.OpenSubscriptionDeliveryTopic(ctx)
	if err != nil {
		s.logger.Ctx(ctx).Error("failed to open subscription", zap.Error(err))
		return err
	}

	for {
		msg, err := subscription.Receive(ctx)
		if err != nil {
			// Errors from Receive indicate that Receive will no longer succeed.
			s.logger.Ctx(ctx).Error("failed to receive message", zap.Error(err))
			break
		}
		// Do work based on the message, for example:
		fmt.Printf("Got message: %q\n", msg.Body)
		// Messages must always be acknowledged with Ack.
		msg.Ack()
	}

	return nil
}
