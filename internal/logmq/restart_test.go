package logmq_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedSubscription hands out msgs, then fails every Receive once they run
// out (failAfter) or blocks until ctx is done.
type scriptedSubscription struct {
	mu        sync.Mutex
	msgs      []*mqs.Message
	failAfter bool
	shutdown  bool
}

func (s *scriptedSubscription) Receive(ctx context.Context) (*mqs.Message, error) {
	s.mu.Lock()
	if len(s.msgs) > 0 {
		m := s.msgs[0]
		s.msgs = s.msgs[1:]
		s.mu.Unlock()
		return m, nil
	}
	failAfter := s.failAfter
	s.mu.Unlock()
	if failAfter {
		return nil, errors.New("connection lost")
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *scriptedSubscription) Shutdown(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdown = true
	return nil
}

// A consumer run that ends while its messages still sit in the batcher (as
// when the supervisor restarts the worker) must not lose them or ack them
// early; the batcher outlives the run and flushes them, and the next run
// keeps using it.
func TestBatchProcessor_SurvivesConsumerRestart(t *testing.T) {
	h := newHarness(t, harnessConfig{batcher: batcherConfig{delay: 500 * time.Millisecond}})
	handler := logmq.NewMessageHandler(testutil.CreateTestLogger(t), h.bp)

	newMsgs := func(dest string, n int) ([]*countingMessage, []*mqs.Message) {
		var counts []*countingMessage
		var msgs []*mqs.Message
		for range n {
			entry := makeEntry(dest, "tenant-1", "atm_"+testutil.RandomString(8), models.AttemptStatusSuccess)
			c, m := newCountingMessage(entry)
			counts = append(counts, c)
			msgs = append(msgs, m)
		}
		return counts, msgs
	}

	firstCounts, firstMsgs := newMsgs("dest-1", 3)
	sub1 := &scriptedSubscription{msgs: firstMsgs, failAfter: true}
	err := consumer.New(sub1, handler,
		consumer.WithMaxConsecutiveErrors(3),
		consumer.WithInitialBackoff(time.Millisecond),
	).Run(context.Background())
	require.Error(t, err, "first run ends on receive errors")
	assert.True(t, sub1.shutdown)
	for _, c := range firstCounts {
		assert.Zero(t, c.acks()+c.nacks(), "not acked before the batch is flushed")
	}

	secondCounts, secondMsgs := newMsgs("dest-2", 2)
	sub2 := &scriptedSubscription{msgs: secondMsgs}
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- consumer.New(sub2, handler).Run(ctx) }()

	all := append(firstCounts, secondCounts...)
	h.waitTerminal(all)
	for _, c := range all {
		c.requireAcked(t)
	}
	assert.Len(t, h.listAttempt("dest-1"), 3)
	assert.Len(t, h.listAttempt("dest-2"), 2)

	cancel()
	require.ErrorIs(t, <-runErr, context.Canceled)
}
