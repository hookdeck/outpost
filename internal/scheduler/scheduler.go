package scheduler

import (
	"context"
	"time"

	"github.com/hookdeck/EventKit/internal/redis"
)

type Scheduler interface {
	Schedule(context.Context, string, time.Time) error
	Monitor(context.Context) error
}

type schedulerImpl struct {
	redisClient *redis.Client
	exec        func(context.Context, string, time.Time) error
}

func New(redisClient *redis.Client, exec func(context.Context, string, time.Time) error) Scheduler {
	return &schedulerImpl{
		redisClient: redisClient,
		exec:        exec,
	}
}

func (s *schedulerImpl) Schedule(ctx context.Context, id string, scheduledAt time.Time) error {
	return nil
}

func (s *schedulerImpl) Monitor(context.Context) error {
	return nil
}
