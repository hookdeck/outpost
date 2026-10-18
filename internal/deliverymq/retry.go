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

// RetryEventGetter is the interface for fetching prior attempts from logstore.
// ListAttempt returns both the attempt and the associated event data.
type RetryEventGetter interface {
	ListAttempt(ctx context.Context, request logstore.ListAttemptRequest) (logstore.ListAttemptResponse, error)
}

// defaultRetryMaxReceiveCount bounds receives of a retry task before it is
// moved to the "deliverymq-retry-dlq" queue; separate from the delivery retry
// max limit.
const defaultRetryMaxReceiveCount = 5

// RetrySchedulerOption is a functional option for configuring the retry scheduler.
type RetrySchedulerOption func(*retrySchedulerConfig)

type retrySchedulerConfig struct {
	visibilityTimeout uint
	maxConcurrency    int
	maxReceiveCount   uint64
}

// WithRetryMaxReceiveCount sets how many times a retry task is received before
// it is moved to the dead-letter queue.
func WithRetryMaxReceiveCount(n uint64) RetrySchedulerOption {
	return func(c *retrySchedulerConfig) {
		c.maxReceiveCount = n
	}
}

// WithRetryMaxConcurrency bounds concurrent retry executions per process.
// 0 means unbounded.
func WithRetryMaxConcurrency(n int) RetrySchedulerOption {
	return func(c *retrySchedulerConfig) {
		c.maxConcurrency = n
	}
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
	cfg := &retrySchedulerConfig{maxReceiveCount: defaultRetryMaxReceiveCount}
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

		// Fetch prior attempt from logstore (single source of truth for both
		// event data and attempt number). A retry always has at least one prior
		// attempt — the one that failed and triggered this retry.
		attemptResp, err := eventGetter.ListAttempt(ctx, retryTask.attemptLookupRequest())
		if err != nil {
			if logger != nil {
				logger.Ctx(ctx).Error("failed to list attempts for retry",
					zap.Error(err),
					zap.String("event_id", retryTask.EventID),
					zap.String("tenant_id", retryTask.TenantID),
					zap.String("destination_id", retryTask.DestinationID))
			}
			return err
		}
		if len(attemptResp.Data) == 0 {
			// No prior attempt found — may be race condition with logmq batching delay.
			// Return error so scheduler retries later.
			if logger != nil {
				logger.Ctx(ctx).Warn("no prior attempt found in logstore, will retry",
					zap.String("event_id", retryTask.EventID),
					zap.String("tenant_id", retryTask.TenantID),
					zap.String("destination_id", retryTask.DestinationID))
			}
			return fmt.Errorf("no prior attempt found in logstore")
		}

		record := attemptResp.Data[0]
		deliveryTask := retryTask.ToDeliveryTask(*record.Event, record.Attempt.AttemptNumber+1)
		if err := deliverymq.Publish(ctx, deliveryTask); err != nil {
			return err
		}
		return nil
	}

	schedulerOpts := []scheduler.Option{
		scheduler.WithPollBackoff(pollBackoff),
		scheduler.WithLogger(logger),
		scheduler.WithMaxReceiveCount(cfg.maxReceiveCount),
		scheduler.WithMaxConcurrency(cfg.maxConcurrency),
	}
	if cfg.visibilityTimeout > 0 {
		schedulerOpts = append(schedulerOpts, scheduler.WithVisibilityTimeout(cfg.visibilityTimeout))
	}
	return scheduler.New("deliverymq-retry", rsmqClient, exec, schedulerOpts...), nil
}

// RetryTask contains the minimal info needed to retry a delivery.
// The full Event data will be fetched from logstore when the retry executes.
type RetryTask struct {
	EventID       string
	TenantID      string
	DestinationID string
	Telemetry     *models.DeliveryTelemetry
	// AttemptTime bounds the log store lookup; zero for tasks queued before
	// it was added.
	AttemptTime time.Time `json:",omitzero"`
}

// retryLookupSkew absorbs the precision lost when the attempt time is stored.
const retryLookupSkew = time.Second

func (m *RetryTask) attemptLookupRequest() logstore.ListAttemptRequest {
	req := logstore.ListAttemptRequest{
		TenantIDs:      []string{m.TenantID},
		EventIDs:       []string{m.EventID},
		DestinationIDs: []string{m.DestinationID},
		Limit:          1,
		SortOrder:      "desc",
	}
	// Tasks scheduled by v1.6.0 and earlier carry no AttemptTime; look them up
	// unbounded.
	if !m.AttemptTime.IsZero() {
		gte := m.AttemptTime.Add(-retryLookupSkew)
		req.TimeFilter.GTE = &gte
	}
	return req
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

func (m *RetryTask) ToDeliveryTask(event models.Event, attemptNumber int) models.DeliveryTask {
	return models.DeliveryTask{
		Attempt:       attemptNumber,
		DestinationID: m.DestinationID,
		Event:         event,
		Telemetry:     m.Telemetry,
	}
}

func RetryTaskFromDeliveryTask(task models.DeliveryTask, attemptTime time.Time) RetryTask {
	return RetryTask{
		AttemptTime:   attemptTime,
		EventID:       task.Event.ID,
		TenantID:      task.Event.TenantID,
		DestinationID: task.DestinationID,
		Telemetry:     task.Telemetry,
	}
}
