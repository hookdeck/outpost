package deliverymq_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/logstore"
	iredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
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

// receiveDLQ advances the fake clock and takes the message from the retry
// scheduler's dead-letter queue.
func receiveDLQ(t *testing.T, client iredis.Client) *rsmq.QueueMessage {
	t.Helper()
	advance(10 * time.Second)
	q := rsmq.NewRedisSMQ(rsmq.NewRedisAdapter(client), "rsmq")
	msg, err := q.ReceiveMessage("deliverymq-retry-dlq", rsmq.UnsetVt)
	require.NoError(t, err)
	require.NotNil(t, msg, "task should land in the retry DLQ")
	return msg
}

func TestRetryScheduler_MaxReceiveCountHonored(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		eventGetter := &countingEventGetter{err: errors.New("log store unavailable")}
		client, schedule := startNotReadyScheduler(t, eventGetter,
			deliverymq.WithRetryVisibilityTimeout(1),
			deliverymq.WithRetryMaxReceiveCount(2))

		task := deliverymq.RetryTask{EventID: "e", TenantID: "t", DestinationID: "d"}
		schedule(task)
		body, err := task.ToString()
		require.NoError(t, err)

		msg := receiveDLQ(t, client)
		assert.Equal(t, body, msg.Message)
		assert.EqualValues(t, 2, eventGetter.calls.Load())
	})
}
