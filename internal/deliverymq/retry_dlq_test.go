package deliverymq_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/mqs"
	iredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingEventGetter struct {
	calls atomic.Int64
	err   error
}

func (g *countingEventGetter) ListAttempt(context.Context, logstore.ListAttemptRequest) (logstore.ListAttemptResponse, error) {
	g.calls.Add(1)
	return logstore.ListAttemptResponse{}, g.err
}

// receiveDLQ polls the retry scheduler's dead-letter queue until it holds a
// message.
func receiveDLQ(t *testing.T, redisConfig *iredis.RedisConfig) *rsmq.QueueMessage {
	t.Helper()
	client, err := iredis.New(context.Background(), redisConfig)
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })
	q := rsmq.NewRedisSMQ(rsmq.NewRedisAdapter(client), "rsmq")
	var got *rsmq.QueueMessage
	require.Eventually(t, func() bool {
		msg, err := q.ReceiveMessage("deliverymq-retry-dlq", rsmq.UnsetVt)
		if err != nil || msg == nil {
			return false
		}
		got = msg
		return true
	}, 10*time.Second, 50*time.Millisecond, "task should land in the retry DLQ")
	return got
}

func TestRetryScheduler_MaxReceiveCountHonored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redisConfig := testutil.CreateTestRedisConfig(t)
	eventGetter := &countingEventGetter{err: errors.New("log store unavailable")}
	dmq := deliverymq.New(deliverymq.WithQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}))
	cleanup, err := dmq.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	s, err := deliverymq.NewRetryScheduler(dmq, redisConfig, "", 10*time.Millisecond, testutil.CreateTestLogger(t), eventGetter,
		deliverymq.WithRetryVisibilityTimeout(1),
		deliverymq.WithRetryMaxReceiveCount(2))
	require.NoError(t, err)
	require.NoError(t, s.Init(ctx))
	go s.Monitor(ctx)
	defer s.Shutdown()

	task := deliverymq.RetryTask{EventID: "e", TenantID: "t", DestinationID: "d"}
	body, err := task.ToString()
	require.NoError(t, err)
	require.NoError(t, s.Schedule(ctx, body, 0))

	msg := receiveDLQ(t, redisConfig)
	assert.Equal(t, body, msg.Message)
	assert.EqualValues(t, 2, eventGetter.calls.Load())
}
