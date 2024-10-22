package deliverymq

import (
	"context"
	"log"

	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/hookdeck/EventKit/internal/scheduler"
)

func NewRetryScheduler(deliverymq *DeliveryMQ, redisConfig *redis.RedisConfig) scheduler.Scheduler {
	exec := func(ctx context.Context, id string) error {
		log.Println("retrying...", id)
		return nil
	}
	return scheduler.New("deliverymq-retry", redisConfig, exec)
}
