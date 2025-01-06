package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis"
	iredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
)

type ScheduleOption func(*scheduleOptions)

type scheduleOptions struct {
	id string
}

func WithTaskID(id string) ScheduleOption {
	return func(o *scheduleOptions) {
		o.id = id
	}
}

type Scheduler interface {
	Init(context.Context) error
	Schedule(context.Context, string, time.Duration, ...ScheduleOption) error
	Monitor(context.Context) error
	Cancel(context.Context, string) error
	Shutdown() error
}

type schedulerImpl struct {
	rsmqClient *rsmq.RedisSMQ
	config     *config
	name       string
	exec       func(context.Context, string) error
}

type config struct {
	visibilityTimeout uint
}

func WithVisibilityTimeout(vt uint) func(*config) {
	return func(c *config) {
		c.visibilityTimeout = vt
	}
}

func New(name string, redisConfig *iredis.RedisConfig, exec func(context.Context, string) error, opts ...func(*config)) Scheduler {
	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", redisConfig.Host, redisConfig.Port),
		Password: redisConfig.Password,
		DB:       redisConfig.Database,
	})
	rsmqClient := rsmq.NewRedisSMQ(redisClient, "rsmq")
	config := &config{
		visibilityTimeout: rsmq.UnsetVt,
	}
	for _, opt := range opts {
		opt(config)
	}
	return &schedulerImpl{
		rsmqClient: rsmqClient,
		config:     config,
		name:       name,
		exec:       exec,
	}
}

func (s *schedulerImpl) Init(ctx context.Context) error {
	if err := s.rsmqClient.CreateQueue(s.name, s.config.visibilityTimeout, rsmq.UnsetDelay, rsmq.UnsetMaxsize); err != nil && err != rsmq.ErrQueueExists {
		return err
	}
	return nil
}

func (s *schedulerImpl) Schedule(ctx context.Context, task string, delay time.Duration, opts ...ScheduleOption) error {
	// Parse options
	options := &scheduleOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Convert delay to seconds and round up
	delaySeconds := uint(delay.Seconds() + 0.5)

	// Generate RSMQ ID if not provided
	var rsmqOpts []rsmq.SendMessageOption
	if options.id != "" {
		rsmqID := generateRSMQID(options.id)
		rsmqOpts = append(rsmqOpts, rsmq.WithMessageID(rsmqID))
	}

	// Send message
	_, err := s.rsmqClient.SendMessage(s.name, task, delaySeconds, rsmqOpts...)
	return err
}

func (s *schedulerImpl) Monitor(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg, err := s.rsmqClient.ReceiveMessage(s.name, rsmq.UnsetVt)
			if err != nil {
				return err
			}
			if msg == nil {
				time.Sleep(time.Second / 10)
				continue
			}
			// TODO: consider using a worker pool to limit the number of concurrent executions
			go func() {
				if err := s.exec(ctx, msg.Message); err != nil {
					return
				}
				if err := s.rsmqClient.DeleteMessage(s.name, msg.ID); err != nil {
					return
				}
			}()
		}
	}
}

func (s *schedulerImpl) Cancel(ctx context.Context, taskID string) error {
	// Generate the RSMQ ID for this task
	rsmqID := generateRSMQID(taskID)

	// Delete the message - RSMQ returns ErrMessageNotFound if it doesn't exist
	err := s.rsmqClient.DeleteMessage(s.name, rsmqID)
	if err == rsmq.ErrMessageNotFound {
		return nil // Task already gone is not an error
	}
	return err
}

func (s *schedulerImpl) Shutdown() error {
	return s.rsmqClient.Quit()
}
