package mqs_test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSubscription struct {
	msgs      chan *mqs.Message
	shutdowns atomic.Int32
}

func newFakeSubscription() *fakeSubscription {
	return &fakeSubscription{msgs: make(chan *mqs.Message, 16)}
}

func (s *fakeSubscription) Receive(ctx context.Context) (*mqs.Message, error) {
	select {
	case msg := <-s.msgs:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *fakeSubscription) Shutdown(context.Context) error {
	s.shutdowns.Add(1)
	return nil
}

func (s *fakeSubscription) push(size int) *fakeMessage {
	qm := &fakeMessage{}
	s.msgs <- &mqs.Message{QueueMessage: qm, Body: make([]byte, size)}
	return qm
}

func (s *fakeSubscription) pushRejectable(size int) *fakeRejectableMessage {
	qm := &fakeRejectableMessage{}
	s.msgs <- &mqs.Message{QueueMessage: qm, Body: make([]byte, size)}
	return qm
}

type fakeMessage struct {
	acks, nacks atomic.Int32
}

func (m *fakeMessage) Ack()  { m.acks.Add(1) }
func (m *fakeMessage) Nack() { m.nacks.Add(1) }

type fakeRejectableMessage struct {
	fakeMessage
	rejects atomic.Int32
}

func (m *fakeRejectableMessage) Reject() { m.rejects.Add(1) }

type receiveResult struct {
	msg *mqs.Message
	err error
}

func receiveAsync(ctx context.Context, sub mqs.Subscription) <-chan receiveResult {
	ch := make(chan receiveResult, 1)
	go func() {
		msg, err := sub.Receive(ctx)
		ch <- receiveResult{msg, err}
	}()
	return ch
}

func mustReceive(t *testing.T, sub mqs.Subscription) *mqs.Message {
	t.Helper()
	msg, err := sub.Receive(t.Context())
	require.NoError(t, err)
	return msg
}

// requireBlocked asserts the receive is still waiting once every goroutine in
// the synctest bubble is idle.
func requireBlocked(t *testing.T, ch <-chan receiveResult) {
	t.Helper()
	synctest.Wait()
	select {
	case res := <-ch:
		t.Fatalf("receive returned while over the limit: msg=%v err=%v", res.msg, res.err)
	default:
	}
}

func requireReceived(t *testing.T, ch <-chan receiveResult) *mqs.Message {
	t.Helper()
	synctest.Wait()
	select {
	case res := <-ch:
		require.NoError(t, res.err)
		return res.msg
	default:
		t.Fatal("receive still waiting")
		return nil
	}
}

func TestLimitBytes_OffReturnsSameSubscription(t *testing.T) {
	t.Parallel()

	inner := newFakeSubscription()
	assert.Same(t, inner, mqs.LimitBytes(inner, 0))
	assert.Same(t, inner, mqs.LimitBytes(inner, -1))
	assert.NotSame(t, inner, mqs.LimitBytes(inner, 1))

	ctx := context.Background()
	queue := mqs.NewQueue(&mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: "bytelimit-off"}})
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	for name, opts := range map[string][]mqs.SubscribeOption{
		"no option": nil,
		"zero":      {mqs.WithConcurrency(5), mqs.WithMaxBytes(0)},
	} {
		sub, err := queue.Subscribe(ctx, opts...)
		require.NoError(t, err)
		assert.IsType(t, &mqs.WrappedSubscription{}, sub, name)
		require.NoError(t, sub.Shutdown(ctx))
	}

	sub, err := queue.Subscribe(ctx, mqs.WithMaxBytes(10))
	require.NoError(t, err)
	_, unwrapped := sub.(*mqs.WrappedSubscription)
	assert.False(t, unwrapped)
	require.NoError(t, sub.Shutdown(ctx))
}

func TestLimitBytes_BlocksUntilSettled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		settle func(*mqs.Message)
		want   [3]int32 // acks, nacks, rejects on the broker message
	}{
		{name: "ack", settle: (*mqs.Message).Ack, want: [3]int32{1, 0, 0}},
		{name: "nack", settle: (*mqs.Message).Nack, want: [3]int32{0, 1, 0}},
		{name: "reject", settle: (*mqs.Message).Reject, want: [3]int32{0, 0, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inner := newFakeSubscription()
				sub := mqs.LimitBytes(inner, 10)

				first := inner.pushRejectable(4)
				inner.pushRejectable(4)
				inner.pushRejectable(4)

				m1 := mustReceive(t, sub)
				mustReceive(t, sub)

				third := receiveAsync(t.Context(), sub)
				requireBlocked(t, third)

				tt.settle(m1)
				requireReceived(t, third)
				assert.Equal(t, tt.want, [3]int32{first.acks.Load(), first.nacks.Load(), first.rejects.Load()})
			})
		})
	}
}

func TestLimitBytes_DoubleSettleReleasesOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		inner := newFakeSubscription()
		sub := mqs.LimitBytes(inner, 10)

		inner.push(6)
		inner.push(6)
		inner.push(6)

		m1 := mustReceive(t, sub)
		second := receiveAsync(t.Context(), sub)
		requireBlocked(t, second)

		m1.Ack()
		m2 := requireReceived(t, second)

		// A second settle of m1 must not free the bytes m2 holds.
		m1.Ack()
		m1.Nack()
		third := receiveAsync(t.Context(), sub)
		requireBlocked(t, third)

		m2.Ack()
		requireReceived(t, third)
	})
}

func TestLimitBytes_OversizedRunsAlone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		inner := newFakeSubscription()
		sub := mqs.LimitBytes(inner, 10)

		inner.push(4)
		inner.push(25)
		inner.push(1)

		small := mustReceive(t, sub)

		oversized := receiveAsync(t.Context(), sub)
		requireBlocked(t, oversized)

		small.Ack()
		big := requireReceived(t, oversized)
		require.Len(t, big.Body, 25)

		next := receiveAsync(t.Context(), sub)
		requireBlocked(t, next)

		big.Ack()
		requireReceived(t, next)
	})
}

func TestLimitBytes_ContextCancelUnblocksWaitingReceive(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		inner := newFakeSubscription()
		sub := mqs.LimitBytes(inner, 10)

		inner.push(8)
		waitingMsg := inner.push(8)

		m1 := mustReceive(t, sub)

		ctx, cancel := context.WithCancel(t.Context())
		waiting := receiveAsync(ctx, sub)
		requireBlocked(t, waiting)

		cancel()
		synctest.Wait()
		res := <-waiting
		require.ErrorIs(t, res.err, context.Canceled)
		require.Nil(t, res.msg)
		assert.Equal(t, int32(1), waitingMsg.nacks.Load(), "waiting message goes back to the broker")
		assert.Equal(t, int32(0), waitingMsg.acks.Load())

		// The cancelled message never held bytes: the limit is intact.
		inner.push(8)
		next := receiveAsync(t.Context(), sub)
		requireBlocked(t, next)
		m1.Ack()
		requireReceived(t, next)
	})
}

func TestLimitBytes_ShutdownUnblocksWaitingReceive(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		inner := newFakeSubscription()
		sub := mqs.LimitBytes(inner, 10)

		inner.push(8)
		waitingMsg := inner.push(8)

		m1 := mustReceive(t, sub)
		waiting := receiveAsync(t.Context(), sub)
		requireBlocked(t, waiting)

		require.NoError(t, sub.Shutdown(t.Context()))
		res := <-waiting
		require.Error(t, res.err)
		require.Nil(t, res.msg)
		assert.Equal(t, int32(1), waitingMsg.nacks.Load(), "waiting message goes back to the broker")
		assert.Equal(t, int32(1), inner.shutdowns.Load())

		// In-flight messages can still be settled after shutdown.
		m1.Ack()
	})
}

func TestLimitBytes_KeepsRejectable(t *testing.T) {
	t.Parallel()

	inner := newFakeSubscription()
	sub := mqs.LimitBytes(inner, 10)

	rejectable := inner.pushRejectable(1)
	plain := inner.push(1)

	m1 := mustReceive(t, sub)
	assert.True(t, m1.Rejectable())
	m1.Reject()
	assert.Equal(t, int32(1), rejectable.rejects.Load())
	assert.Equal(t, int32(0), rejectable.nacks.Load())

	m2 := mustReceive(t, sub)
	assert.False(t, m2.Rejectable())
	m2.Reject()
	assert.Equal(t, int32(1), plain.nacks.Load())
}
