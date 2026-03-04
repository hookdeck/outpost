package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/rsmq"
	"go.uber.org/zap"
)

type ScheduleOption func(*ScheduleOptions)

type ScheduleOptions struct {
	ID string
}

func WithTaskID(id string) ScheduleOption {
	return func(o *ScheduleOptions) {
		o.ID = id
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
	rsmqClient rsmq.Client
	config     *config
	name       string
	exec       func(context.Context, string) error
}

type config struct {
	visibilityTimeout    uint
	pollBackoff          time.Duration
	maxConsecutiveErrors int
	maxErrorBackoff      time.Duration
	logger               *logging.Logger
}

func WithVisibilityTimeout(vt uint) func(*config) {
	return func(c *config) {
		c.visibilityTimeout = vt
	}
}

func WithPollBackoff(backoff time.Duration) func(*config) {
	return func(c *config) {
		c.pollBackoff = backoff
	}
}

func WithMaxConsecutiveErrors(n int) func(*config) {
	return func(c *config) {
		c.maxConsecutiveErrors = n
	}
}

func WithLogger(logger *logging.Logger) func(*config) {
	return func(c *config) {
		c.logger = logger
	}
}

func New(name string, rsmqClient rsmq.Client, exec func(context.Context, string) error, opts ...func(*config)) Scheduler {
	config := &config{
		visibilityTimeout:    rsmq.UnsetVt,
		pollBackoff:          200 * time.Millisecond,
		maxConsecutiveErrors: 5,
		maxErrorBackoff:      5 * time.Second,
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
	options := &ScheduleOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Convert delay to seconds and round up
	delaySeconds := uint(delay.Seconds() + 0.5)

	// Generate RSMQ ID if not provided
	var rsmqOpts []rsmq.SendMessageOption
	if options.ID != "" {
		rsmqID := generateRSMQID(options.ID)
		rsmqOpts = append(rsmqOpts, rsmq.WithMessageID(rsmqID))
	}

	// Send message
	_, err := s.rsmqClient.SendMessage(s.name, task, delaySeconds, rsmqOpts...)
	return err
}

func (s *schedulerImpl) Monitor(ctx context.Context) error {
	consecutiveErrors := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg, err := s.rsmqClient.ReceiveMessage(s.name, rsmq.UnsetVt)
			if err != nil {
				consecutiveErrors++
				if consecutiveErrors >= s.config.maxConsecutiveErrors {
					return fmt.Errorf("max consecutive errors reached: %w", err)
				}
				backoff := s.config.pollBackoff * time.Duration(1<<(consecutiveErrors-1))
				if backoff > s.config.maxErrorBackoff {
					backoff = s.config.maxErrorBackoff
				}
				s.config.logger.Ctx(ctx).Warn("scheduler receive error, retrying",
					zap.Error(err),
					zap.Int("attempt", consecutiveErrors),
					zap.Duration("backoff", backoff))
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(backoff):
				}
				continue
			}
			consecutiveErrors = 0
			if msg == nil {
				time.Sleep(s.config.pollBackoff)
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
