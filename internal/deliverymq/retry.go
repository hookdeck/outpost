package deliverymq

import (
	"context"
	"encoding/json"
	"errors"
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

// defaultRetryNotReadyDelay matches the default log flush interval.
const defaultRetryNotReadyDelay = 10 * time.Second

// maxAttemptWriteLag is how long an attempt's log write may lag before the
// retry stops waiting for it.
const maxAttemptWriteLag = 15 * time.Minute

// maxNotReadyDelay caps the wait between lookups for an attempt not stored yet.
const maxNotReadyDelay = 5 * time.Minute

var errNoPriorAttempt = errors.New("no prior attempt found in logstore")

// RetrySchedulerOption is a functional option for configuring the retry scheduler.
type RetrySchedulerOption func(*retrySchedulerConfig)

type retrySchedulerConfig struct {
	visibilityTimeout uint
	maxConcurrency    int
	maxReceiveCount   uint64
	notReadyDelay     time.Duration
	lookupBatchSize   int
	lookupBatchWait   time.Duration
}

// WithRetryLookupBatch batches the attempt lookups of retries executing at the
// same time into one log store query: it flushes at size lookups or wait after
// the first one. A size below 2 disables batching, as does a log store without
// batch support.
func WithRetryLookupBatch(size int, wait time.Duration) RetrySchedulerOption {
	return func(c *retrySchedulerConfig) {
		c.lookupBatchSize = size
		c.lookupBatchWait = wait
	}
}

// WithRetryNotReadyDelay sets how long a retry waits when the attempt that
// triggered it is not in the log store yet; set it to the log flush interval.
func WithRetryNotReadyDelay(d time.Duration) RetrySchedulerOption {
	return func(c *retrySchedulerConfig) {
		c.notReadyDelay = d
	}
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
	cfg := &retrySchedulerConfig{maxReceiveCount: defaultRetryMaxReceiveCount, notReadyDelay: defaultRetryNotReadyDelay}
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

	lookup, batcher, err := newRetryLookup(eventGetter, effectiveLookupBatchSize(cfg.lookupBatchSize, cfg.maxConcurrency), cfg.lookupBatchWait)
	if err != nil {
		return nil, fmt.Errorf("failed to create retry lookup batcher: %w", err)
	}

	exec := func(ctx context.Context, msg string) error {
		retryTask := RetryTask{}
		if err := retryTask.FromString(msg); err != nil {
			return err
		}

		// Fetch prior attempt from logstore (single source of truth for both
		// event data and attempt number). A retry always has at least one prior
		// attempt — the one that failed and triggered this retry.
		record, err := lookup(ctx, retryTask)
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
		if record == nil {
			return noAttemptResult(ctx, logger, retryTask, cfg.notReadyDelay)
		}

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
	retryScheduler := scheduler.New("deliverymq-retry", rsmqClient, exec, schedulerOpts...)
	if batcher != nil {
		return batchingScheduler{Scheduler: retryScheduler, batcher: batcher}, nil
	}
	return retryScheduler, nil
}

// noAttemptResult handles a lookup that finds no attempt.
//
// Known race, accepted: attempts reach the log store in batches, and nothing
// orders the retry after that write, so a retry due before the flush runs
// before its attempt is stored:
//
//	delivery handler          retry scheduler            log store
//	----------------          ---------------            ---------
//	attempt fails
//	logmq.Publish(attempt) -------------------------->  batched
//	Schedule(retry, 1s) --->  receive retry
//	                          ListAttempt: not found
//	                          requeue, not counted
//	                                                     flush (up to
//	                                                     LOG_BATCH_THRESHOLD_SECONDS)
//	                          receive retry
//	                          ListAttempt: found, publish
//
// The task is requeued without counting the receive until a grace window
// since the attempt passes; after that it is a counted error with the normal
// backoff. Tasks scheduled by v1.6.0 and earlier carry no AttemptTime and are
// always a counted error.
func noAttemptResult(ctx context.Context, logger *logging.Logger, task RetryTask, notReadyDelay time.Duration) error {
	if task.AttemptTime.IsZero() {
		if logger != nil {
			logger.Ctx(ctx).Warn("no prior attempt found in logstore, will retry",
				zap.String("event_id", task.EventID),
				zap.String("tenant_id", task.TenantID),
				zap.String("destination_id", task.DestinationID))
		}
		return errNoPriorAttempt
	}
	grace := max(maxAttemptWriteLag, 3*notReadyDelay)
	age := time.Since(task.AttemptTime)
	if age > grace {
		if logger != nil {
			logger.Ctx(ctx).Error("no prior attempt found in logstore within grace window",
				zap.String("event_id", task.EventID),
				zap.String("tenant_id", task.TenantID),
				zap.String("destination_id", task.DestinationID),
				zap.Time("attempt_time", task.AttemptTime),
				zap.Duration("grace", grace))
		}
		return errNoPriorAttempt
	}
	if logger != nil {
		logger.Ctx(ctx).Debug("prior attempt not in logstore yet, requeueing",
			zap.String("event_id", task.EventID),
			zap.String("tenant_id", task.TenantID),
			zap.String("destination_id", task.DestinationID))
	}
	return scheduler.RetryLater(notReadyRequeueDelay(age, notReadyDelay), errNoPriorAttempt)
}

// notReadyRequeueDelay waits about half the attempt's age: a short lag
// resolves on the first requeue, a long one costs a few lookups.
func notReadyRequeueDelay(age, notReadyDelay time.Duration) time.Duration {
	return min(max(notReadyDelay, age/2, time.Second), maxNotReadyDelay)
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
