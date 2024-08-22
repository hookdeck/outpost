package delivery

import (
	"context"
	"log"
	"sync"
)

type DeliveryService struct{}

func NewService(ctx context.Context, wg *sync.WaitGroup) *DeliveryService {
	go func() {
		defer wg.Done()
		<-ctx.Done()
		log.Println("shutting down data service")
	}()
	return &DeliveryService{}
}

func (s *DeliveryService) Run(ctx context.Context) error {
	log.Println("running delivery service")
	return nil
}
