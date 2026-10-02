package mqs_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
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
	ackSizes  []int         // ack ids of every Acknowledge call
	ackErrs   []error       // returned by the next Acknowledge calls
	ackGate   chan struct{} // when set, Acknowledge waits for it to close
	pullErrs  []error       // returned by the next Pull calls
	noWait    bool          // answer an empty backlog at once
	delay     time.Duration // round trip of a pull
	respBytes int           // most body bytes in one response, 0 for no cap
	extra     int           // messages a pull returns beyond max_messages
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
	delay := f.delay
	f.requested = append(f.requested, n)
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	}
	f.mu.Lock()
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
	k := min(n+f.extra, len(f.backlog))
	if f.respBytes > 0 {
		total := 0
		for i, rm := range f.backlog[:k] {
			total += len(rm.Message.Data)
			if total > f.respBytes && i > 0 {
				k = i
				break
			}
		}
	}
	resp := &pubsubpb.PullResponse{ReceivedMessages: f.backlog[:k:k]}
	f.backlog = f.backlog[k:]
	return resp, nil
}

func (f *fakeGCPAPI) Acknowledge(ctx context.Context, req *pubsubpb.AcknowledgeRequest, _ ...gax.CallOption) error {
	f.mu.Lock()
	gate := f.ackGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ackCalls++
	f.ackSizes = append(f.ackSizes, len(req.AckIds))
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

// Large messages arrive after a small one. The pull in flight was counted
// with the small size: what it had asked for comes in and is handled, over
// the limit. Nothing is nacked, nothing more is pulled until there is room,
// and from then on the large size is what a pull reserves.
func TestGCPPull_SizeJumpOvershootsByPullsInFlight(t *testing.T) {
	t.Parallel()
	const (
		small  = 10
		large  = 600
		limit  = 1000
		larges = 20
	)
	api := newFakeGCPAPI()
	api.publish(small)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(100), mqs.WithMaxBytes(limit))

	first := receiveN(t, sub, 1)[0]
	// Everything the count limit leaves, counted at the small size.
	require.Eventually(t, func() bool { return mqs.GCPPullStateOf(sub).Requested == 99 }, 5*time.Second, 5*time.Millisecond)

	api.publish(repeatSize(larges, large)...)
	requireHeld(t, sub, 1+larges)
	state := mqs.GCPPullStateOf(sub)
	assert.Zero(t, state.Requested)
	assert.Equal(t, int64(small+larges*large), state.HeldBytes)
	_, _, _, nacked := api.snapshot()
	assert.Empty(t, nacked)

	// Back under the limit: one large message at a time.
	api.publish(repeatSize(5, large)...)
	first.Ack()
	for _, msg := range receiveN(t, sub, larges) {
		msg.Ack()
	}
	for backlog := 5; backlog > 0; {
		requireHeld(t, sub, 1)
		receiveN(t, sub, 1)[0].Ack()
		_, backlog, _, _ = api.snapshot()
	}
}

// drainFake acks every message as it is received, after hold, and returns
// once count messages are acked.
func drainFake(t *testing.T, sub mqs.Subscription, count int, hold time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for range count {
		msg, err := sub.Receive(ctx)
		require.NoError(t, err)
		if hold == 0 {
			msg.Ack()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(hold)
			msg.Ack()
		}()
	}
	wg.Wait()
}

func repeatSize(count, size int) []int {
	sizes := make([]int, count)
	for i := range sizes {
		sizes[i] = size
	}
	return sizes
}

// A deep backlog of small messages is taken in a few large pulls: handlers
// finishing one by one do not turn into pulls for one message each, and a
// short pull that answers late does not shrink the ones after it.
func TestGCPPull_SmallBacklogTakesFewPulls(t *testing.T) {
	t.Parallel()
	const count = 5000
	api := newFakeGCPAPI()
	api.delay = 5 * time.Millisecond
	api.publish(repeatSize(count, 1000)...)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(1100), mqs.WithMaxBytes(16<<20))

	drainFake(t, sub, count, 0)
	requested, backlog, _, _ := api.snapshot()
	assert.Zero(t, backlog)
	// A quarter of 1000 per pull once the size is known.
	assert.LessOrEqual(t, len(requested), 5+2*count/250, "pulls: %v", requested)
	api.mu.Lock()
	defer api.mu.Unlock()
	assert.LessOrEqual(t, api.maxFlight, 1000)
}

// One large message does not hold small ones back: a few pulls after it the
// small ones are pulled as if it had not been there.
func TestGCPPull_SmallAfterLarge(t *testing.T) {
	t.Parallel()
	const (
		count = 3000
		limit = 16 << 20
	)
	for name, large := range map[string]int{"1 MiB": 1 << 20, "9 MiB": 9 << 20} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := newFakeGCPAPI()
			api.delay = 5 * time.Millisecond
			api.publish(large)
			api.publish(repeatSize(count, 1000)...)
			sub, _ := newFakePull(t, api, mqs.WithConcurrency(1100), mqs.WithMaxBytes(limit))

			start := time.Now()
			drainFake(t, sub, 1+count, 0)
			assert.Less(t, time.Since(start), 5*time.Second)
			requested, _, _, _ := api.snapshot()
			assert.LessOrEqual(t, len(requested), 12, "pulls: %v", requested)
			// No pull for the few messages that would fit at the large size
			// while a pull for many is out.
			assert.GreaterOrEqual(t, slices.Min(requested), 50, "pulls: %v", requested)
		})
	}
}

// Small messages with a large one every hundred: pulls stay large, and what
// the process holds stays near the limit.
func TestGCPPull_InterleavedSizes(t *testing.T) {
	t.Parallel()
	const (
		count = 3000
		large = 1 << 20
		limit = 16 << 20
	)
	api := newFakeGCPAPI()
	api.delay = 5 * time.Millisecond
	sizes := repeatSize(count, 1000)
	for i := 50; i < count; i += 100 {
		sizes[i] = large
	}
	api.publish(sizes...)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(1100), mqs.WithMaxBytes(limit))

	var peak atomic.Int64
	stop := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
				peak.Store(max(peak.Load(), mqs.GCPPullStateOf(sub).HeldBytes))
			}
		}
	}()
	drainFake(t, sub, count, 20*time.Millisecond)
	close(stop)
	<-sampled

	requested, _, _, _ := api.snapshot()
	assert.LessOrEqual(t, len(requested), 80, "pulls: %v", requested)
	assert.Greater(t, peak.Load(), int64(limit/4))
	assert.LessOrEqual(t, peak.Load(), int64(limit+4*large))
}

// With nothing known about sizes, as at the start or after a quiet period,
// pulls for the whole count limit wait at the service, and a burst is taken
// in pulls of that size: no pull for one message first, no growing from the
// last response.
func TestGCPPull_IdleThenBurst(t *testing.T) {
	t.Parallel()
	const (
		count = 500
		burst = 1000
	)
	api := newFakeGCPAPI()
	api.delay = 5 * time.Millisecond
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(count), mqs.WithMaxBytes(64<<20))
	require.Eventually(t, func() bool {
		requested, _, _, _ := api.snapshot()
		return len(requested) == 4
	}, 5*time.Second, 5*time.Millisecond)
	requested, _, _, _ := api.snapshot()
	assert.Equal(t, []int{125, 125, 125, 125}, requested)

	api.publish(repeatSize(burst, 1000)...)
	drainFake(t, sub, burst, 0)
	requested, _, _, _ = api.snapshot()
	assert.LessOrEqual(t, len(requested), 4+3*burst/125, "pulls: %v", requested)
	small := 0
	for _, n := range requested {
		if n < 125 {
			small++
		}
	}
	// A pull for less than a share goes out only when no other is in flight.
	assert.LessOrEqual(t, small, 4, "pulls: %v", requested)
	// Pulls wait again, for the whole count limit unless one of the four went
	// out alone for less than a share.
	require.Eventually(t, func() bool {
		state := mqs.GCPPullStateOf(sub)
		return state.HeldCount == 0 && state.Requested > count-125
	}, 5*time.Second, 5*time.Millisecond)
}

// A message published to an idle subscription is received from a pull that
// is already waiting, not after a pull round trip.
func TestGCPPull_IdleMessageArrivesAtOnce(t *testing.T) {
	t.Parallel()
	api := newFakeGCPAPI()
	api.delay = 500 * time.Millisecond
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(500), mqs.WithMaxBytes(64<<20))
	require.Eventually(t, func() bool {
		api.mu.Lock()
		defer api.mu.Unlock()
		return api.inFlight == 500
	}, 5*time.Second, 5*time.Millisecond, "pulls waiting at the service")

	for range 3 {
		published := time.Now()
		api.publish(1000)
		receiveN(t, sub, 1)[0].Ack()
		assert.Less(t, time.Since(published), 250*time.Millisecond)
	}
}

// While a pull is in flight, the next one waits until it can ask for a whole
// share of what the limits allow.
func TestGCPPull_WaitsForAShare(t *testing.T) {
	t.Parallel()
	const share = 100 // a quarter of the count limit
	api := newFakeGCPAPI()
	api.publish(repeatSize(2000, 10)...)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(4*share), mqs.WithMaxBytes(1<<30))
	requireHeld(t, sub, 4*share)
	msgs := receiveN(t, sub, 4*share)
	before, _, _, _ := api.snapshot()

	api.mu.Lock()
	api.delay = time.Second
	api.mu.Unlock()

	// Nothing in flight: the first free place is asked for at once.
	msgs[0].Ack()
	for _, msg := range msgs[1:share] {
		msg.Ack()
	}
	time.Sleep(100 * time.Millisecond)
	requested, _, _, _ := api.snapshot()
	require.Equal(t, []int{1}, requested[len(before):])

	msgs[share].Ack()
	require.Eventually(t, func() bool {
		requested, _, _, _ := api.snapshot()
		return len(requested) == len(before)+2
	}, time.Second, 5*time.Millisecond)
	requested, _, _, _ = api.snapshot()
	assert.Equal(t, []int{1, share}, requested[len(before):])
}

// A limit above what one response carries, and a backlog of large messages.
// The first pull, with no size to go by, is counted as a whole response and
// brings one. After that the process takes exactly what fits.
func TestGCPPull_LargeBacklogFillsLimit(t *testing.T) {
	t.Parallel()
	const (
		large = 1 << 20
		limit = 16 << 20
	)
	api := newFakeGCPAPI()
	api.respBytes = 10 << 20
	api.publish(repeatSize(60, large)...)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(1100), mqs.WithMaxBytes(limit))

	requireHeld(t, sub, 16)
	state := mqs.GCPPullStateOf(sub)
	assert.Zero(t, state.Requested)
	assert.Equal(t, int64(limit), state.HeldBytes)

	for _, msg := range receiveN(t, sub, 3) {
		msg.Ack()
	}
	requireHeld(t, sub, 16)
	_, backlog, _, nacked := api.snapshot()
	assert.Equal(t, 60-19, backlog)
	assert.Empty(t, nacked)
}

// A response larger than responses are expected to be: from then on a pull
// reserves that much.
func TestGCPPull_LearnsResponseSize(t *testing.T) {
	t.Parallel()
	const (
		large    = 1 << 20
		limit    = 64 << 20
		expected = 10 << 20
	)
	api := newFakeGCPAPI()
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(1100), mqs.WithMaxBytes(limit))
	drain := func(count int) {
		t.Helper()
		api.publish(repeatSize(count, large)...)
		for _, msg := range receiveN(t, sub, count) {
			msg.Ack()
		}
	}

	drain(1)
	drain(8)
	// Pulls for many messages are out, each reserving one response.
	require.Eventually(t, func() bool {
		state := mqs.GCPPullStateOf(sub)
		return state.Requested >= 250 && state.RequestedBytes <= 4*expected
	}, 5*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.LessOrEqual(t, mqs.GCPPullStateOf(sub).RequestedBytes, int64(4*expected))

	assert.Equal(t, int64(expected), mqs.GCPPullStateOf(sub).ResponseBytes)

	// One of them comes back with more than 20 MiB. From then on that is
	// what a pull for many messages reserves.
	drain(30)
	learned := mqs.GCPPullStateOf(sub).ResponseBytes
	require.Greater(t, learned, int64(20*large))
	for range 4 {
		drain(1)
	}
	time.Sleep(100 * time.Millisecond)
	state := mqs.GCPPullStateOf(sub)
	assert.Equal(t, learned, state.ResponseBytes)
	assert.LessOrEqual(t, state.RequestedBytes, int64(limit))
}

// Messages beyond what a pull asked for go back when there is no room.
func TestGCPPull_ResponseLargerThanAsked(t *testing.T) {
	t.Parallel()
	api := newFakeGCPAPI()
	api.extra = 3
	api.publish(10, 10, 10, 10)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(2), mqs.WithMaxBytes(1000))

	require.Eventually(t, func() bool {
		_, _, _, nacked := api.snapshot()
		return len(nacked) == 2
	}, 5*time.Second, 5*time.Millisecond)
	requireHeld(t, sub, 2)
	_, backlog, _, nacked := api.snapshot()
	assert.Zero(t, backlog)
	assert.ElementsMatch(t, []string{"ack-3", "ack-4"}, nacked)
}

func TestGCPPull_DefaultCountLimit(t *testing.T) {
	t.Parallel()
	api := newFakeGCPAPI()
	api.publish(repeatSize(1500, 10)...)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(0), mqs.WithMaxBytes(1<<30))
	requireHeld(t, sub, 1000)
}

// Acks made while a request is in flight go out together, at most 1000 ids
// per request.
func TestGCPPull_AckBatches(t *testing.T) {
	t.Parallel()
	const count = 1500
	api := newFakeGCPAPI()
	api.ackGate = make(chan struct{})
	api.publish(repeatSize(count, 10)...)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(count), mqs.WithMaxBytes(1<<30))

	msgs := receiveN(t, sub, count)
	msgs[0].Ack()
	time.Sleep(50 * time.Millisecond)
	for _, msg := range msgs[1:] {
		msg.Ack()
	}
	close(api.ackGate)
	require.Eventually(t, func() bool {
		_, _, acked, _ := api.snapshot()
		return len(acked) == count
	}, 5*time.Second, 5*time.Millisecond)
	api.mu.Lock()
	defer api.mu.Unlock()
	assert.LessOrEqual(t, len(api.ackSizes), 4)
	for _, size := range api.ackSizes {
		assert.LessOrEqual(t, size, 1000)
	}
}

// An ack request that keeps failing is given up after three attempts, and
// the acks after it are still sent.
func TestGCPPull_AckGivenUp(t *testing.T) {
	t.Parallel()
	api := newFakeGCPAPI()
	failed := status.Error(codes.Internal, "down")
	api.ackErrs = []error{failed, failed, failed}
	api.publish(10, 10)
	sub, _ := newFakePull(t, api, mqs.WithConcurrency(10), mqs.WithMaxBytes(1000))

	msgs := receiveN(t, sub, 2)
	msgs[0].Ack()
	require.Eventually(t, func() bool {
		api.mu.Lock()
		defer api.mu.Unlock()
		return api.ackCalls == 3
	}, 5*time.Second, 5*time.Millisecond)
	msgs[1].Ack()
	require.Eventually(t, func() bool {
		_, _, acked, _ := api.snapshot()
		return len(acked) == 1
	}, 5*time.Second, 5*time.Millisecond)
	_, _, acked, _ := api.snapshot()
	assert.Equal(t, []string{"ack-2"}, acked)
	api.mu.Lock()
	defer api.mu.Unlock()
	assert.Equal(t, 4, api.ackCalls)
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

	t.Run("long polls, no more requests", func(t *testing.T) {
		t.Parallel()
		api := newFakeGCPAPI()
		sub, _ := newFakePull(t, api, mqs.WithConcurrency(1000), mqs.WithMaxBytes(1<<20))
		time.Sleep(300 * time.Millisecond)
		requested, _, _, _ := api.snapshot()
		// The limit is under one response: one message, to learn the size.
		assert.Equal(t, []int{1}, requested)

		large := newFakeGCPAPI()
		newFakePull(t, large, mqs.WithConcurrency(1000), mqs.WithMaxBytes(64<<20))
		time.Sleep(300 * time.Millisecond)
		requested, _, _, _ = large.snapshot()
		assert.Equal(t, []int{250, 250, 250, 250}, requested)

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
