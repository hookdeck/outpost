package delivery

import (
	"context"
	"log"
	"sync"

	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

type DeliveryService struct {
	logger *otelzap.Logger
}

func NewService(ctx context.Context, wg *sync.WaitGroup, logger *otelzap.Logger) *DeliveryService {
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		log.Println("shutting down log service")
	}()
	return &DeliveryService{
		logger: logger,
	}
}

func (s *DeliveryService) Run(ctx context.Context) error {
	s.logger.Ctx(ctx).Info("running delivery service")
	return nil
}
