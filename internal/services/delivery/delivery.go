package delivery

import (
	"context"
	"sync"

	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type DeliveryService struct {
	logger *otelzap.Logger
}

func NewService(ctx context.Context, wg *sync.WaitGroup, logger *otelzap.Logger) *DeliveryService {
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		logger.Ctx(ctx).Info("service shutdown", zap.String("service", "delivery"))
	}()
	return &DeliveryService{
		logger: logger,
	}
}

func (s *DeliveryService) Run(ctx context.Context) error {
	s.logger.Ctx(ctx).Info("start service", zap.String("service", "delivery"))
	return nil
}
