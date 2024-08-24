package log

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type LogService struct {
	logger *otelzap.Logger
}

func NewService(ctx context.Context, wg *sync.WaitGroup, logger *otelzap.Logger) *LogService {
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		logger.Ctx(ctx).Info("service shutdown", zap.String("service", "log"))
	}()
	return &LogService{
		logger: logger,
	}
}

func (s *LogService) Run(ctx context.Context) error {
	s.logger.Ctx(ctx).Info("start service", zap.String("service", "log"))

	if os.Getenv("DISABLED") == "true" {
		s.logger.Ctx(ctx).Info("service is disabled", zap.String("service", "log"))
		return nil
	}

	for range time.Tick(time.Second * 1) {
		keys, err := redis.Client().Keys(ctx, "destination:*").Result()
		if err != nil {
			s.logger.Ctx(ctx).Error("error",
				zap.Error(err),
			)
			continue
		}
		s.logger.Ctx(ctx).Info("destination count", zap.Int("count", len(keys)))
	}

	return nil
}
