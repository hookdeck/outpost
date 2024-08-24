package data

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

type DataService struct {
	logger *otelzap.Logger
}

func NewService(ctx context.Context, wg *sync.WaitGroup, logger *otelzap.Logger) *DataService {
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		log.Println("shutting down data service")
	}()
	return &DataService{
		logger: logger,
	}
}

func (s *DataService) Run(ctx context.Context) error {
	log.Println("running data service")

	if os.Getenv("DISABLED") == "true" {
		log.Println("data service is disabled")
		return nil
	}

	for range time.Tick(time.Second * 1) {
		keys, err := redis.Client().Keys(ctx, "destination:*").Result()
		if err != nil {
			log.Println(err)
			continue
		}
		log.Printf("%d destination(s)\n", len(keys))
	}

	return nil
}
