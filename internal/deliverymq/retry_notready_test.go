package deliverymq_test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	iredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startNotReadyScheduler must run inside a synctest bubble. Its cleanup stops
// every goroutine it started, so the bubble can exit.
func startNotReadyScheduler(t *testing.T, getter deliverymq.RetryEventGetter, opts ...deliverymq.RetrySchedulerOption) (client iredis.Client, schedule func(deliverymq.RetryTask)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	_, redisConfig, client := testutil.NewSynctestRedis(t)
	dmq := deliverymq.New(deliverymq.WithQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(16)}}))
	cleanup, err := dmq.Init(ctx)
	require.NoError(t, err)

	s, err := deliverymq.NewRetryScheduler(dmq, redisConfig, "", 10*time.Millisecond, testutil.CreateTestLogger(t), getter, opts...)
	require.NoError(t, err)
	require.NoError(t, s.Init(ctx))
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		s.Monitor(ctx)
	}()
	t.Cleanup(func() { stopRetryStack(nil, nil, cancel, monitorDone, s, cleanup) })

	return client, func(task deliverymq.RetryTask) {
		body, err := task.ToString()
		require.NoError(t, err)
		require.NoError(t, s.Schedule(ctx, body, 0))
	}
}

func TestRetryScheduler_AttemptNotPersistedYetIsNotCounted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		event := testutil.EventFactory.Any(
			testutil.EventFactory.WithDestinationID("dest_1"),
			testutil.EventFactory.WithEligibleForRetry(true),
		)
		// Three misses exceed the max receive count of 2; they must not count.
		getter := newMockDelayedEventGetter(&event, 3)
		client, schedule := startNotReadyScheduler(t, getter,
			deliverymq.WithRetryMaxReceiveCount(2),
			deliverymq.WithRetryNotReadyDelay(0))

		schedule(deliverymq.RetryTaskFromDeliveryTask(models.DeliveryTask{Event: event, DestinationID: event.DestinationID}, time.Now()))

		advance(15 * time.Second)
		require.GreaterOrEqual(t, getter.calls(), 4, "lookup should keep being retried past the max receive count")

		q := rsmq.NewRedisSMQ(rsmq.NewRedisAdapter(client), "rsmq")
		msg, err := q.ReceiveMessage("deliverymq-retry-dlq", rsmq.UnsetVt)
		require.NoError(t, err)
		assert.Nil(t, msg, "not-ready receives must not dead-letter the task")
	})
}

func TestRetryScheduler_AttemptMissingPastGraceIsCounted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		getter := &countingEventGetter{}
		client, schedule := startNotReadyScheduler(t, getter,
			deliverymq.WithRetryVisibilityTimeout(1),
			deliverymq.WithRetryMaxReceiveCount(2))

		schedule(deliverymq.RetryTask{EventID: "e", TenantID: "t", DestinationID: "d", AttemptTime: time.Now().Add(-time.Hour)})

		msg := receiveDLQ(t, client)
		assert.Contains(t, msg.Message, `"EventID":"e"`)
		assert.EqualValues(t, 2, getter.calls.Load(), "dead-lettered after max receive count")
	})
}

func TestRetryScheduler_MissingAttemptWithoutAttemptTimeIsCounted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		getter := &countingEventGetter{}
		client, schedule := startNotReadyScheduler(t, getter,
			deliverymq.WithRetryVisibilityTimeout(1),
			deliverymq.WithRetryMaxReceiveCount(2))

		schedule(deliverymq.RetryTask{EventID: "e", TenantID: "t", DestinationID: "d"})

		receiveDLQ(t, client)
		assert.EqualValues(t, 2, getter.calls.Load())
	})
}

// incompleteRecordGetter returns a record that lacks its event or attempt.
type incompleteRecordGetter struct {
	calls   atomic.Int64
	records []*logstore.AttemptRecord
}

func (g *incompleteRecordGetter) ListAttempt(context.Context, logstore.ListAttemptRequest) (logstore.ListAttemptResponse, error) {
	g.calls.Add(1)
	return logstore.ListAttemptResponse{Data: g.records}, nil
}

func TestRetryScheduler_RecordWithoutEventOrAttemptIsCounted(t *testing.T) {
	t.Parallel()
	cases := map[string]*logstore.AttemptRecord{
		"nil event":   {Attempt: &models.Attempt{ID: "a", AttemptNumber: 1}},
		"nil attempt": {Event: &models.Event{ID: "e"}},
	}
	for name, record := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				getter := &incompleteRecordGetter{records: []*logstore.AttemptRecord{record}}
				client, schedule := startNotReadyScheduler(t, getter,
					deliverymq.WithRetryVisibilityTimeout(1),
					deliverymq.WithRetryMaxReceiveCount(2))

				schedule(deliverymq.RetryTask{EventID: "e", TenantID: "t", DestinationID: "d", AttemptTime: time.Now()})

				receiveDLQ(t, client)
				assert.EqualValues(t, 2, getter.calls.Load(), "dead-lettered after max receive count")
			})
		})
	}
}
