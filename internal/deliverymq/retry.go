package deliverymq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/hookdeck/outpost/internal/scheduler"
	"go.uber.org/zap"
)

// RetryEventGetter is the interface for fetching events from logstore.
// This is defined separately from EventGetter in messagehandler.go to avoid circular dependencies.
type RetryEventGetter interface {
	RetrieveEvent(ctx context.Context, request logstore.RetrieveEventRequest) (*models.Event, error)
}

// RetrySchedulerOption is a functional option for configuring the retry scheduler.
type RetrySchedulerOption func(*retrySchedulerConfig)

type retrySchedulerConfig struct {
	visibilityTimeout uint
}

// WithRetryVisibilityTimeout sets the visibility timeout for the retry scheduler queue.
// This controls how long a message is hidden after being received before it becomes
// visible again (for retry if the executor returned an error).
func WithRetryVisibilityTimeout(vt uint) RetrySchedulerOption {
	return func(c *retrySchedulerConfig) {
		c.visibilityTimeout = vt
	}
}

func NewRetryScheduler(deliverymq *DeliveryMQ, redisConfig *redis.RedisConfig, deploymentID string, pollBackoff time.Duration, logger *logging.Logger, eventGetter RetryEventGetter, opts ...RetrySchedulerOption) (scheduler.Scheduler, error) {
	cfg := &retrySchedulerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	ctx := context.Background()
	redisClient, err := redis.New(ctx, redisConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis client for retry scheduler: %w", err)
	}

	adapter := rsmq.NewRedisAdapter(redisClient)

	// Construct RSMQ namespace with deployment prefix if provided
	// This creates keys like: dp_001:rsmq:QUEUES, dp_001:rsmq:deliverymq-retry:Q
	// Without deployment ID: rsmq:QUEUES, rsmq:deliverymq-retry:Q
	namespace := "rsmq"
	if deploymentID != "" {
		namespace = fmt.Sprintf("%s:rsmq", deploymentID)
	}

	var rsmqClient *rsmq.RedisSMQ
	if logger != nil {
		rsmqClient = rsmq.NewRedisSMQ(adapter, namespace, logger)
	} else {
		rsmqClient = rsmq.NewRedisSMQ(adapter, namespace)
	}

	exec := func(ctx context.Context, msg string) error {
		retryTask := RetryTask{}
		if err := retryTask.FromString(msg); err != nil {
			return err
		}

		// Fetch full event data from logstore
		event, err := eventGetter.RetrieveEvent(ctx, logstore.RetrieveEventRequest{
			TenantID: retryTask.TenantID,
			EventID:  retryTask.EventID,
		})
		if err != nil {
			// Returning an error leaves the message in the RSMQ queue. After the
			// visibility timeout expires, the message becomes visible again and will
			// be reprocessed. This handles both transient DB errors and the race
			// condition where logmq hasn't flushed the event yet.
			if logger != nil {
				logger.Ctx(ctx).Error("failed to fetch event for retry",
					zap.Error(err),
					zap.String("event_id", retryTask.EventID),
					zap.String("tenant_id", retryTask.TenantID),
					zap.String("destination_id", retryTask.DestinationID))
			}
			return err
		}
		if event == nil {
			// Event not found - may be race condition with logmq batching delay.
			// Return error so scheduler retries later.
			if logger != nil {
				logger.Ctx(ctx).Warn("event not found in logstore, will retry",
					zap.String("event_id", retryTask.EventID),
					zap.String("tenant_id", retryTask.TenantID),
					zap.String("destination_id", retryTask.DestinationID))
			}
			return fmt.Errorf("event not found in logstore")
		}

		deliveryTask := retryTask.ToDeliveryTask(*event)
		if err := deliverymq.Publish(ctx, deliveryTask); err != nil {
			return err
		}
		return nil
	}

	if cfg.visibilityTimeout > 0 {
		return scheduler.New("deliverymq-retry", rsmqClient, exec,
			scheduler.WithPollBackoff(pollBackoff),
			scheduler.WithVisibilityTimeout(cfg.visibilityTimeout)), nil
	}
	return scheduler.New("deliverymq-retry", rsmqClient, exec, scheduler.WithPollBackoff(pollBackoff)), nil
}

// RetryTask contains the minimal info needed to retry a delivery.
// The full Event data will be fetched from logstore when the retry executes.
type RetryTask struct {
	EventID       string
	TenantID      string
	DestinationID string
	Attempt       int
	Telemetry     *models.DeliveryTelemetry
}

func (m *RetryTask) ToString() (string, error) {
	json, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(json), nil
}

func (m *RetryTask) FromString(str string) error {
	return json.Unmarshal([]byte(str), &m)
}

func (m *RetryTask) ToDeliveryTask(event models.Event) models.DeliveryTask {
	return models.DeliveryTask{
		Attempt:       m.Attempt,
		DestinationID: m.DestinationID,
		Event:         event,
		Telemetry:     m.Telemetry,
	}
}

func RetryTaskFromDeliveryTask(task models.DeliveryTask) RetryTask {
	return RetryTask{
		EventID:       task.Event.ID,
		TenantID:      task.Event.TenantID,
		DestinationID: task.DestinationID,
		Attempt:       task.Attempt + 1,
		Telemetry:     task.Telemetry,
	}
}
