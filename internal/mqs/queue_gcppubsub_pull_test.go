package mqs_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"github.com/googleapis/gax-go/v2"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeGCPAPI is a subscription that hands out its backlog in order, at most
// max_messages per pull, and long-polls while the backlog is empty.
type fakeGCPAPI struct {
	mu        sync.Mutex
	backlog   []*pubsubpb.ReceivedMessage
	arrived   chan struct{}
	next      int
	requested []int // max_messages of every pull
	inFlight  int   // max_messages of the pulls waiting for messages
	maxFlight int
	acked     []string
	nacked    []string
	ackCalls  int
	ackErrs   []error // returned by the next Acknowledge calls
	pullErrs  []error // returned by the next Pull calls
	noWait    bool    // answer an empty backlog at once
	closed    int
}

func newFakeGCPAPI() *fakeGCPAPI {
	return &fakeGCPAPI{arrived: make(chan struct{})}
}

func (f *fakeGCPAPI) publish(sizes ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, size := range sizes {
		f.next++
		f.backlog = append(f.backlog, &pubsubpb.ReceivedMessage{
			AckId:   fmt.Sprintf("ack-%d", f.next),
			Message: &pubsubpb.PubsubMessage{MessageId: fmt.Sprintf("m-%d", f.next), Data: make([]byte, size)},
		})
	}
	close(f.arrived)
	f.arrived = make(chan struct{})
}

func (f *fakeGCPAPI) Pull(ctx context.Context, req *pubsubpb.PullRequest, _ ...gax.CallOption) (*pubsubpb.PullResponse, error) {
	n := int(req.MaxMessages)
	f.mu.Lock()
	f.requested = append(f.requested, n)
	if len(f.pullErrs) > 0 {
		err := f.pullErrs[0]
		f.pullErrs = f.pullErrs[1:]
		f.mu.Unlock()
		return nil, err
	}
	f.inFlight += n
	f.maxFlight = max(f.maxFlight, f.inFlight)
	defer func() {
		f.inFlight -= n
		f.mu.Unlock()
	}()
	for len(f.backlog) == 0 {
		if f.noWait {
			return &pubsubpb.PullResponse{}, nil
		}
		arrived := f.arrived
		f.mu.Unlock()
		select {
		case <-arrived:
		case <-ctx.Done():
			f.mu.Lock()
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		f.mu.Lock()
	}
	k := min(n, len(f.backlog))
	resp := &pubsubpb.PullResponse{ReceivedMessages: f.backlog[:k:k]}
	f.backlog = f.backlog[k:]
	return resp, nil
}

func (f *fakeGCPAPI) Acknowledge(_ context.Context, req *pubsubpb.AcknowledgeRequest, _ ...gax.CallOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ackCalls++
	if len(f.ackErrs) > 0 {
		err := f.ackErrs[0]
		f.ackErrs = f.ackErrs[1:]
		return err
	}
	f.acked = append(f.acked, req.AckIds...)
	return nil
}

func (f *fakeGCPAPI) ModifyAckDeadline(_ context.Context, req *pubsubpb.ModifyAckDeadlineRequest, _ ...gax.CallOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.AckDeadlineSeconds != 0 {
		return status.Error(codes.InvalidArgument, "only nacks expected")
	}
	f.nacked = append(f.nacked, req.AckIds...)
	return nil
}

func (f *fakeGCPAPI) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

func (f *fakeGCPAPI) snapshot() (requested []int, backlog int, acked, nacked []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.requested...), len(f.backlog), append([]string(nil), f.acked...), append([]string(nil), f.nacked...)
}

func newFakePull(t *testing.T, api *fakeGCPAPI, opts ...mqs.SubscribeOption) (mqs.Subscription, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	sub := mqs.NewGCPPullSubscription(ctx, api, "projects/p/subscriptions/s", opts...)
	t.Cleanup(func() {
		cancel()
		sub.Shutdown(context.Background())
	})
	return sub, cancel
}

// requireHeld waits until the subscription holds want messages, then checks
// that it stays there.
func requireHeld(t *testing.T, sub mqs.Subscription, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return mqs.GCPPullStateOf(sub).HeldCount == want },
		5*time.Second, 5*time.Millisecond, "held %d, want %d", mqs.GCPPullStateOf(sub).HeldCount, want)
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, want, mqs.GCPPullStateOf(sub).HeldCount)
}

func receiveN(t *testing.T, sub mqs.Subscription, n int) []*mqs.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msgs := make([]*mqs.Message, 0, n)
	for range n {
		msg, err := sub.Receive(ctx)
		require.NoError(t, err)
		msgs = append(msgs, msg)
	}
	return msgs
}

// With a deep backlog of equal messages the process takes one message to
// learn the size, then exactly what fits. The rest is never asked for.
func TestGCPPull_DeepBacklogStaysInSubscription(t *testing.T) {
	t.Parallel()
	api := newFakeGCPAPI()
	sizes := make([]int, 64)
	for i := range sizes {
		sizes[i] = 1000
	}
	api.publish(sizes...)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(64), mqs.WithMaxBytes(4500))

	requireHeld(t, sub, 4)
	state := mqs.GCPPullStateOf(sub)
	assert.Equal(t, int64(4000), state.HeldBytes)
	assert.Zero(t, state.Requested, "no pull in flight while the limit is used up")
	requested, backlog, _, nacked := api.snapshot()
	assert.Equal(t, 1, requested[0], "first pull asks for one message")
	assert.Equal(t, 60, backlog)
	assert.Empty(t, nacked)

	// One message settled makes room for exactly one more.
	msgs := receiveN(t, sub, 4)
	msgs[0].Ack()
	requireHeld(t, sub, 4)
	_, backlog, acked, _ := api.snapshot()
	assert.Equal(t, 59, backlog)
	assert.Len(t, acked, 1)
}

func TestGCPPull_CountLimit(t *testing.T) {
	t.Parallel()
	api := newFakeGCPAPI()
	sizes := make([]int, 12)
	for i := range sizes {
		sizes[i] = 10
	}
	api.publish(sizes...)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(2), mqs.WithMaxBytes(1<<30))

	for range 6 {
		requireHeld(t, sub, 2)
		for _, msg := range receiveN(t, sub, 2) {
			msg.Ack()
		}
	}
	requireHeld(t, sub, 0)
	requested, backlog, acked, _ := api.snapshot()
	assert.Zero(t, backlog)
	assert.Len(t, acked, 12)
	for _, n := range requested {
		assert.LessOrEqual(t, n, 2)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	assert.LessOrEqual(t, api.maxFlight, 2)
}

// Messages larger than the whole limit run one at a time.
func TestGCPPull_OversizedRunsAlone(t *testing.T) {
	t.Parallel()
	api := newFakeGCPAPI()
	api.publish(1000, 1000, 1000, 1000)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(50), mqs.WithMaxBytes(500))

	for i := range 4 {
		requireHeld(t, sub, 1)
		_, backlog, _, _ := api.snapshot()
		assert.Equal(t, 3-i, backlog)
		receiveN(t, sub, 1)[0].Ack()
	}
	requireHeld(t, sub, 0)
	requested, _, _, nacked := api.snapshot()
	assert.Empty(t, nacked)
	for _, n := range requested {
		assert.Equal(t, 1, n)
	}
}

// Large messages arrive after small ones set the estimate. What the pulls in
// flight had asked for comes in and is handled, over the limit; nothing is
// nacked, nothing more is pulled until there is room, and from then on the
// large size is what a pull reserves.
func TestGCPPull_SizeJumpOvershootsByPullsInFlight(t *testing.T) {
	t.Parallel()
	const (
		small = 10
		large = 600
		limit = 1000
	)
	api := newFakeGCPAPI()
	api.publish(small)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(100), mqs.WithMaxBytes(limit))

	first := receiveN(t, sub, 1)[0]
	// After one message came back: up to four pulls of two.
	require.Eventually(t, func() bool { return mqs.GCPPullStateOf(sub).Requested == 8 }, 5*time.Second, 5*time.Millisecond)

	sizes := make([]int, 20)
	for i := range sizes {
		sizes[i] = large
	}
	api.publish(sizes...)

	require.Eventually(t, func() bool { return mqs.GCPPullStateOf(sub).Requested == 0 }, 5*time.Second, 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	state := mqs.GCPPullStateOf(sub)
	assert.Zero(t, state.Requested)
	assert.LessOrEqual(t, state.HeldCount, 1+8)
	assert.Greater(t, state.HeldBytes, int64(limit))
	assert.LessOrEqual(t, state.HeldBytes, int64(limit+8*large))
	_, backlog, _, nacked := api.snapshot()
	assert.Equal(t, 20-(state.HeldCount-1), backlog)
	assert.Empty(t, nacked)

	first.Ack()
	for _, msg := range receiveN(t, sub, state.HeldCount-1) {
		msg.Ack()
	}
	for backlog > 0 {
		requireHeld(t, sub, 1)
		receiveN(t, sub, 1)[0].Ack()
		_, backlog, _, _ = api.snapshot()
	}
}

func TestGCPPull_AckAndNack(t *testing.T) {
	t.Parallel()
	api := newFakeGCPAPI()
	api.ackErrs = []error{status.Error(codes.Internal, "try again"), status.Error(codes.Internal, "try again")}
	api.publish(10, 10, 10)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(10), mqs.WithMaxBytes(1000))

	msgs := receiveN(t, sub, 3)
	msgs[0].Ack()
	msgs[0].Ack()
	msgs[0].Nack()
	msgs[1].Nack()
	require.Eventually(t, func() bool {
		_, _, acked, nacked := api.snapshot()
		return len(acked) == 1 && len(nacked) == 1
	}, 5*time.Second, 10*time.Millisecond, "a failed ack request is retried")
	_, _, acked, nacked := api.snapshot()
	assert.Equal(t, []string{"ack-1"}, acked)
	assert.Equal(t, []string{"ack-2"}, nacked)
	assert.Equal(t, 1, mqs.GCPPullStateOf(sub).HeldCount)
	assert.False(t, msgs[2].Rejectable())
}

// When the context ends, messages nobody has received are nacked at once,
// while a handler is still running. Shutdown sends that handler's ack before
// it closes the client.
func TestGCPPull_ShutdownReturnsWaitingMessages(t *testing.T) {
	t.Parallel()
	api := newFakeGCPAPI()
	api.publish(10, 10, 10)
	sub, cancel := newFakePull(t, api, mqs.WithConcurrency(10), mqs.WithMaxBytes(1000))

	inHandler := receiveN(t, sub, 1)[0]
	requireHeld(t, sub, 3)

	cancel()
	require.Eventually(t, func() bool {
		_, _, _, nacked := api.snapshot()
		return len(nacked) == 2
	}, 2*time.Second, 5*time.Millisecond, "waiting messages are nacked before shutdown")
	_, err := sub.Receive(context.Background())
	require.ErrorContains(t, err, "subscription closed")

	inHandler.Ack()
	require.NoError(t, sub.Shutdown(context.Background()))
	_, _, acked, nacked := api.snapshot()
	assert.Equal(t, []string{"ack-1"}, acked)
	assert.ElementsMatch(t, []string{"ack-2", "ack-3"}, nacked)
	api.mu.Lock()
	defer api.mu.Unlock()
	assert.Equal(t, 1, api.closed)
}

// An ack made just before Shutdown is sent before Shutdown returns.
func TestGCPPull_ShutdownSendsPendingAcks(t *testing.T) {
	t.Parallel()
	for range 20 {
		api := newFakeGCPAPI()
		api.publish(10)
		sub, cancel := newFakePull(t, api, mqs.WithConcurrency(10), mqs.WithMaxBytes(1000))
		msg := receiveN(t, sub, 1)[0]
		// As in a consumer: the context ends, the handler finishes, Shutdown.
		cancel()
		_, err := sub.Receive(context.Background())
		require.ErrorContains(t, err, "subscription closed")
		msg.Ack()
		require.NoError(t, sub.Shutdown(context.Background()))
		_, _, acked, _ := api.snapshot()
		require.Equal(t, []string{"ack-1"}, acked)
	}
}

func TestGCPPull_ShutdownWithUnsettledMessage(t *testing.T) {
	t.Parallel()
	api := newFakeGCPAPI()
	api.publish(10)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(10), mqs.WithMaxBytes(1000))
	receiveN(t, sub, 1)

	start := time.Now()
	require.NoError(t, sub.Shutdown(context.Background()))
	assert.Less(t, time.Since(start), time.Second)
	_, _, acked, nacked := api.snapshot()
	assert.Empty(t, acked)
	assert.Empty(t, nacked)
}

func TestGCPPull_Idle(t *testing.T) {
	t.Parallel()

	t.Run("one long poll for one message", func(t *testing.T) {
		t.Parallel()
		api := newFakeGCPAPI()
		sub, _ := newFakePull(t, api, mqs.WithConcurrency(1000), mqs.WithMaxBytes(1<<20))
		time.Sleep(300 * time.Millisecond)
		requested, _, _, _ := api.snapshot()
		assert.Equal(t, []int{1}, requested)

		start := time.Now()
		require.NoError(t, sub.Shutdown(context.Background()))
		assert.Less(t, time.Since(start), time.Second, "shutdown does not wait for the long poll")
	})

	t.Run("no spin when empty pulls return at once", func(t *testing.T) {
		t.Parallel()
		api := newFakeGCPAPI()
		api.noWait = true
		newFakePull(t, api, mqs.WithConcurrency(1000), mqs.WithMaxBytes(1<<20))
		time.Sleep(time.Second)
		requested, _, _, _ := api.snapshot()
		assert.LessOrEqual(t, len(requested), 6)
		assert.GreaterOrEqual(t, len(requested), 2)
	})
}

func TestGCPPull_Errors(t *testing.T) {
	t.Parallel()

	t.Run("transient error is retried", func(t *testing.T) {
		t.Parallel()
		api := newFakeGCPAPI()
		api.pullErrs = []error{status.Error(codes.Unavailable, "down")}
		api.publish(10)
		sub, _ := newFakePull(t, api, mqs.WithConcurrency(10), mqs.WithMaxBytes(1000))
		receiveN(t, sub, 1)[0].Ack()
	})

	t.Run("long poll deadline is an empty pull", func(t *testing.T) {
		t.Parallel()
		api := newFakeGCPAPI()
		api.pullErrs = []error{status.Error(codes.DeadlineExceeded, "deadline")}
		api.publish(10)
		sub, _ := newFakePull(t, api, mqs.WithConcurrency(10), mqs.WithMaxBytes(1000))
		receiveN(t, sub, 1)[0].Ack()
	})

	t.Run("other errors close the subscription", func(t *testing.T) {
		t.Parallel()
		api := newFakeGCPAPI()
		api.pullErrs = []error{status.Error(codes.NotFound, "no such subscription")}
		sub, _ := newFakePull(t, api, mqs.WithConcurrency(10), mqs.WithMaxBytes(1000))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := sub.Receive(ctx)
		require.ErrorContains(t, err, "subscription closed")
		require.ErrorContains(t, err, "no such subscription")
		time.Sleep(50 * time.Millisecond)
		requested, _, _, _ := api.snapshot()
		assert.Len(t, requested, 1)
	})
}

func TestGCPPull_SizeWindow(t *testing.T) {
	t.Parallel()
	add, largest := mqs.GCPSizeWindow()
	t0 := time.Now()
	period := mqs.GCPPullSizePeriod

	assert.Zero(t, largest(t0), "nothing seen")
	add(t0, 100)
	add(t0.Add(time.Second), 0)
	add(t0.Add(2*time.Second), 40)
	assert.Equal(t, int64(100), largest(t0.Add(3*time.Second)))

	// Kept for at least one period, dropped after two.
	add(t0.Add(period+time.Second), 30)
	assert.Equal(t, int64(100), largest(t0.Add(2*period-time.Second)))
	assert.Equal(t, int64(30), largest(t0.Add(2*period+time.Second)))
	assert.Zero(t, largest(t0.Add(4*period)))

	add(t0.Add(5*period), 0)
	assert.Equal(t, int64(1), largest(t0.Add(5*period)), "an empty body still counts as seen")
}
