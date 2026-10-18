package deliverymq_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	iredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startNotReadyScheduler(t *testing.T, ctx context.Context, getter deliverymq.RetryEventGetter, opts ...deliverymq.RetrySchedulerOption) (redisConfig *iredis.RedisConfig, schedule func(deliverymq.RetryTask)) {
	t.Helper()
	redisConfig = testutil.CreateTestRedisConfig(t)
	dmq := deliverymq.New(deliverymq.WithQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}))
	cleanup, err := dmq.Init(ctx)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	s, err := deliverymq.NewRetryScheduler(dmq, redisConfig, "", 10*time.Millisecond, testutil.CreateTestLogger(t), getter, opts...)
	require.NoError(t, err)
	require.NoError(t, s.Init(ctx))
	go s.Monitor(ctx)
	t.Cleanup(func() { s.Shutdown() })

	return redisConfig, func(task deliverymq.RetryTask) {
		body, err := task.ToString()
		require.NoError(t, err)
		require.NoError(t, s.Schedule(ctx, body, 0))
	}
}

func TestRetryScheduler_AttemptNotPersistedYetIsNotCounted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithDestinationID("dest_1"),
		testutil.EventFactory.WithEligibleForRetry(true),
	)
	// Three misses exceed the max receive count of 2; they must not count.
	getter := newMockDelayedEventGetter(&event, 3)
	redisConfig, schedule := startNotReadyScheduler(t, ctx, getter,
		deliverymq.WithRetryMaxReceiveCount(2),
		deliverymq.WithRetryNotReadyDelay(0))

	schedule(deliverymq.RetryTaskFromDeliveryTask(models.DeliveryTask{Event: event, DestinationID: event.DestinationID}, time.Now()))

	require.Eventually(t, func() bool { return getter.calls() >= 4 }, 15*time.Second, 50*time.Millisecond,
		"lookup should keep being retried past the max receive count")

	client, err := iredis.New(ctx, redisConfig)
	require.NoError(t, err)
	defer client.Close()
	q := rsmq.NewRedisSMQ(rsmq.NewRedisAdapter(client), "rsmq")
	msg, err := q.ReceiveMessage("deliverymq-retry-dlq", rsmq.UnsetVt)
	require.NoError(t, err)
	assert.Nil(t, msg, "not-ready receives must not dead-letter the task")
}

func TestRetryScheduler_AttemptMissingPastGraceIsCounted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	getter := &countingEventGetter{}
	redisConfig, schedule := startNotReadyScheduler(t, ctx, getter,
		deliverymq.WithRetryVisibilityTimeout(1),
		deliverymq.WithRetryMaxReceiveCount(2))

	schedule(deliverymq.RetryTask{EventID: "e", TenantID: "t", DestinationID: "d", AttemptTime: time.Now().Add(-time.Hour)})

	msg := receiveDLQ(t, redisConfig)
	assert.Contains(t, msg.Message, `"EventID":"e"`)
	assert.EqualValues(t, 2, getter.calls.Load(), "dead-lettered after max receive count")
}

func TestRetryScheduler_MissingAttemptWithoutAttemptTimeIsCounted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	getter := &countingEventGetter{}
	redisConfig, schedule := startNotReadyScheduler(t, ctx, getter,
		deliverymq.WithRetryVisibilityTimeout(1),
		deliverymq.WithRetryMaxReceiveCount(2))

	schedule(deliverymq.RetryTask{EventID: "e", TenantID: "t", DestinationID: "d"})

	receiveDLQ(t, redisConfig)
	assert.EqualValues(t, 2, getter.calls.Load())
}
