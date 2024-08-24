package log

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

type LogService struct {
	logger *otelzap.Logger
}

func NewService(ctx context.Context, wg *sync.WaitGroup, logger *otelzap.Logger) *LogService {
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		log.Println("shutting down log service")
	}()
	return &LogService{
		logger: logger,
	}
}

func (s *LogService) Run(ctx context.Context) error {
	s.logger.Ctx(ctx).Info("running log service")

	if os.Getenv("DISABLED") == "true" {
		log.Println("log service is disabled")
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
