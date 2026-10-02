package mqs_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nativepubsub "cloud.google.com/go/pubsub"
	"cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"github.com/googleapis/gax-go/v2"
	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMQMaxBytes_InMemory(t *testing.T) {
	t.Parallel()
	config := mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}
	testMQMaxBytes(t, config, maxBytesCase{enforced: true})
}

func TestIntegrationMQMaxBytes_RabbitMQ(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	testMQMaxBytes(t, testinfra.NewMQRabbitMQConfig(t), maxBytesCase{enforced: true, rejectable: true})
}

func TestIntegrationMQMaxBytes_NATS(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	testMQMaxBytes(t, testinfra.NewMQNATSConfig(t), maxBytesCase{enforced: true})
}

func TestIntegrationMQMaxBytes_AWSSQS(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	testMQMaxBytes(t, testinfra.NewMQAWSConfig(t, nil), maxBytesCase{enforced: true})
}

func TestIntegrationMQMaxBytes_GCPPubSub(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	testMQMaxBytes(t, testinfra.NewMQGCPConfig(t, nil), maxBytesCase{enforced: true, batched: true})
}

func TestIntegrationMQMaxBytes_AzureSB(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	testMQMaxBytes(t, testinfra.GetMQAzureConfig(t, "TestIntegrationMQMaxBytes_AzureSB"), maxBytesCase{enforced: true, rejectable: true})
}

func TestMQMaxBytes_InMemoryShutdownWhileWaiting(t *testing.T) {
	t.Parallel()
	config := mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}
	// A new in-memory subscription only sees messages published after it
	// opens, so redelivery cannot be observed here.
	testMQMaxBytesShutdownWhileWaiting(t, config, false)
}

func TestIntegrationMQMaxBytes_AWSSQSShutdownWhileWaiting(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	testMQMaxBytesShutdownWhileWaiting(t, testinfra.NewMQAWSConfig(t, nil), true)
}

func TestIntegrationMQMaxBytes_GCPPubSubShutdownWhileWaiting(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	// The second message is never pulled while the first holds the limit, so
	// a new subscriber gets it at once.
	testMQMaxBytesShutdownWhileWaiting(t, testinfra.NewMQGCPConfig(t, nil), true)
}

// Without a limit the StreamingPull settings are the ones used before the
// option existed. A limit does not change them: it uses another receive path.
func TestGCPPubSubQueue_MaxBytesLeavesStreamSettings(t *testing.T) {
	t.Parallel()
	queue := mqs.NewQueue(&mqs.QueueConfig{
		GCPPubSub:         &mqs.GCPPubSubConfig{},
		VisibilityTimeout: 30 * time.Second,
	})

	want := nativepubsub.ReceiveSettings{
		MaxOutstandingMessages: 5,
		NumGoroutines:          1,
		MaxExtension:           -1 * time.Second,
		MinExtensionPeriod:     30 * time.Second,
	}
	assert.Equal(t, want, mqs.GCPReceiveSettings(queue, mqs.WithConcurrency(5)))
	assert.Equal(t, want, mqs.GCPReceiveSettings(queue, mqs.WithConcurrency(5), mqs.WithMaxBytes(0)))
	assert.Equal(t, want, mqs.GCPReceiveSettings(queue, mqs.WithConcurrency(5), mqs.WithMaxBytes(1<<20)))
}

// Only a limit switches Pub/Sub to the pull loop. The shared helper is never
// used, and the subscription manages its own concurrency either way.
func TestIntegrationMQMaxBytes_GCPPubSubReceivePath(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQGCPConfig(t, nil)
	ctx := context.Background()
	queue := mqs.NewQueue(&config)

	cases := []struct {
		name string
		opts []mqs.SubscribeOption
		pull bool
	}{
		{name: "no option", opts: []mqs.SubscribeOption{mqs.WithConcurrency(5)}},
		{name: "zero", opts: []mqs.SubscribeOption{mqs.WithConcurrency(5), mqs.WithMaxBytes(0)}},
		{name: "limit", opts: []mqs.SubscribeOption{mqs.WithConcurrency(5), mqs.WithMaxBytes(1 << 20)}, pull: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subscription, err := queue.Subscribe(ctx, tc.opts...)
			require.NoError(t, err)
			defer subscription.Shutdown(ctx)

			assert.Equal(t, tc.pull, mqs.IsGCPPull(subscription))
			assert.False(t, mqs.IsByteLimited(subscription))
			concurrent, ok := subscription.(mqs.ConcurrentSubscription)
			require.True(t, ok)
			assert.True(t, concurrent.SupportsConcurrency())
		})
	}
}

func publishGCPBacklog(t *testing.T, ctx context.Context, queue mqs.Queue, prefix string, count, bodySize int) {
	t.Helper()
	padding := strings.Repeat("x", bodySize)
	for i := range count {
		require.NoError(t, queue.Publish(ctx, &Msg{ID: fmt.Sprintf("%s-%02d", prefix, i), Data: map[string]string{"pad": padding}}))
	}
}

// With a byte limit the count limit is part of what a pull asks for: two
// messages are in the process at a time out of a backlog of twelve.
func TestIntegrationMQMaxBytes_GCPPubSubCountLimit(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQGCPConfig(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	const total = 12
	publishGCPBacklog(t, ctx, queue, "m", total, 16)

	subscription, err := queue.Subscribe(ctx, mqs.WithConcurrency(2), mqs.WithMaxBytes(1<<30))
	require.NoError(t, err)
	defer subscription.Shutdown(context.Background())

	var (
		maxHeld int
		wg      sync.WaitGroup
	)
	for range total {
		msg, err := subscription.Receive(ctx)
		require.NoError(t, err)
		maxHeld = max(maxHeld, mqs.GCPPullStateOf(subscription).HeldCount)
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(200 * time.Millisecond)
			msg.Ack()
		}()
	}
	wg.Wait()
	assert.Equal(t, 2, maxHeld)
}

// A backlog many times the limit: the process receives what fits and asks
// for nothing more. The rest stays in the subscription with no ack deadline
// running, so another subscriber gets all of it right away.
func TestIntegrationMQMaxBytes_GCPPubSubBacklogStaysInSubscription(t *testing.T) {
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQGCPConfig(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	const (
		total    = 64
		bodySize = 1 << 20
		limit    = 4 << 20
		fitting  = 3 // a body is slightly over 1 MiB
		// The backlog is 64 MiB.
		maxGrowth = 8 << 20
	)
	publishGCPBacklog(t, ctx, queue, "m", total, bodySize)
	heap := func() uint64 {
		runtime.GC()
		runtime.GC()
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		return stats.HeapAlloc
	}
	baseline := heap()

	subscription, err := queue.Subscribe(ctx, mqs.WithConcurrency(total), mqs.WithMaxBytes(limit))
	require.NoError(t, err)
	defer subscription.Shutdown(context.Background())

	require.Eventually(t, func() bool { return mqs.GCPPullStateOf(subscription).HeldCount == fitting }, 30*time.Second, 10*time.Millisecond)
	time.Sleep(2 * time.Second)
	state := mqs.GCPPullStateOf(subscription)
	assert.Equal(t, fitting, state.HeldCount)
	assert.LessOrEqual(t, state.HeldBytes, int64(limit))
	assert.Zero(t, state.Requested, "nothing is asked for while the limit is used up")
	growth := int64(heap()) - int64(baseline)
	assert.Less(t, growth, int64(maxGrowth), "heap grew by %d MiB with a %d MiB limit", growth>>20, limit>>20)

	other, err := queue.Subscribe(ctx, mqs.WithConcurrency(total), mqs.WithMaxBytes(1<<30))
	require.NoError(t, err)
	defer other.Shutdown(context.Background())
	// Well inside the subscription's 20s ack deadline.
	otherCtx, otherCancel := context.WithTimeout(ctx, 10*time.Second)
	defer otherCancel()
	for i := range total - fitting {
		msg, err := other.Receive(otherCtx)
		require.NoError(t, err, "message %d of the backlog was not available to another subscriber", i)
		msg.Ack()
	}
	assert.Equal(t, fitting, mqs.GCPPullStateOf(subscription).HeldCount)
}

// Large messages arrive after a small one set the size estimate. The pulls
// in flight bring in what they had asked for, over the limit; after that the
// large size is what a pull reserves.
func TestIntegrationMQMaxBytes_GCPPubSubMixedSizes(t *testing.T) {
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQGCPConfig(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	const (
		larges    = 24
		largeSize = 1 << 20
		limit     = 5 << 19 // 2.5 MiB: two large messages
		inFlight  = 8       // four pulls of two after one message came back
	)
	subscription, err := queue.Subscribe(ctx, mqs.WithConcurrency(100), mqs.WithMaxBytes(limit))
	require.NoError(t, err)
	defer subscription.Shutdown(context.Background())

	publishGCPBacklog(t, ctx, queue, "small", 1, 16)
	small, err := subscription.Receive(ctx)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return mqs.GCPPullStateOf(subscription).Requested == inFlight }, 10*time.Second, 10*time.Millisecond)

	publishGCPBacklog(t, ctx, queue, "large", larges, largeSize)
	require.Eventually(t, func() bool {
		state := mqs.GCPPullStateOf(subscription)
		return state.Requested == 0 && state.HeldBytes > limit
	}, 30*time.Second, 10*time.Millisecond)
	time.Sleep(2 * time.Second)
	state := mqs.GCPPullStateOf(subscription)
	assert.Zero(t, state.Requested)
	assert.LessOrEqual(t, state.HeldCount, 1+inFlight)
	assert.LessOrEqual(t, state.HeldBytes, int64(limit+inFlight*(largeSize+64)))

	small.Ack()
	for range state.HeldCount - 1 {
		msg, err := subscription.Receive(ctx)
		require.NoError(t, err)
		msg.Ack()
	}
	for range larges - (state.HeldCount - 1) {
		msg, err := subscription.Receive(ctx)
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond)
		held := mqs.GCPPullStateOf(subscription)
		assert.LessOrEqual(t, held.HeldCount, 2)
		assert.LessOrEqual(t, held.HeldBytes, int64(limit))
		msg.Ack()
	}
}

// Messages the process has received and no handler has taken go back when
// the context ends, and another subscriber gets them at once.
func TestIntegrationMQMaxBytes_GCPPubSubShutdownReturnsReceived(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQGCPConfig(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(context.Background())
	require.NoError(t, err)
	defer cleanup()

	const total = 3
	publishGCPBacklog(t, ctx, queue, "m", total, 16)
	subscription, err := queue.Subscribe(ctx, mqs.WithConcurrency(5), mqs.WithMaxBytes(1<<20))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return mqs.GCPPullStateOf(subscription).HeldCount == total }, 20*time.Second, 10*time.Millisecond)

	cancel()
	stopped := time.Now()
	require.NoError(t, subscription.Shutdown(context.Background()))
	assert.Less(t, time.Since(stopped), 2*time.Second)

	// Well inside the subscription's 20s ack deadline.
	otherCtx, otherCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer otherCancel()
	other, err := queue.Subscribe(otherCtx, mqs.WithConcurrency(5), mqs.WithMaxBytes(1<<20))
	require.NoError(t, err)
	defer other.Shutdown(context.Background())
	for i := range total {
		msg, err := other.Receive(otherCtx)
		require.NoError(t, err, "message %d was not returned to the subscription", i)
		msg.Ack()
	}
}

// A message that is never settled does not hold up Shutdown. It comes back
// when the subscription's own ack deadline (20s here) passes: nothing extends
// it. An acked message does not come back.
func TestIntegrationMQMaxBytes_GCPPubSubAckDeadline(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQGCPConfig(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	subscription, err := queue.Subscribe(ctx, mqs.WithConcurrency(5), mqs.WithMaxBytes(1<<20))
	require.NoError(t, err)
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "acked"}))
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "unsettled"}))

	received := time.Now()
	for range 2 {
		msg, err := subscription.Receive(ctx)
		require.NoError(t, err)
		parsed := &Msg{}
		require.NoError(t, parsed.FromMessage(msg))
		if parsed.ID == "acked" {
			msg.Ack()
		}
	}
	stopping := time.Now()
	require.NoError(t, subscription.Shutdown(ctx))
	assert.Less(t, time.Since(stopping), 2*time.Second)

	other, err := queue.Subscribe(ctx, mqs.WithConcurrency(5), mqs.WithMaxBytes(1<<20))
	require.NoError(t, err)
	defer other.Shutdown(context.Background())
	msg, err := other.Receive(ctx)
	require.NoError(t, err)
	parsed := &Msg{}
	require.NoError(t, parsed.FromMessage(msg))
	msg.Ack()
	assert.Equal(t, "unsettled", parsed.ID)
	assert.InDelta(t, 20, time.Since(received).Seconds(), 8)

	quietCtx, quietCancel := context.WithTimeout(ctx, 3*time.Second)
	defer quietCancel()
	_, err = other.Receive(quietCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded, "acked message came back")
}

type countingGCPAPI struct {
	mqs.GCPSubscriberAPI
	pulls atomic.Int64
}

func (c *countingGCPAPI) Pull(ctx context.Context, req *pubsubpb.PullRequest, opts ...gax.CallOption) (*pubsubpb.PullResponse, error) {
	c.pulls.Add(1)
	return c.GCPSubscriberAPI.Pull(ctx, req, opts...)
}

// An empty subscription costs a few pull requests, not a busy loop, and a
// message published while idle still arrives quickly.
func TestIntegrationMQMaxBytes_GCPPubSubIdle(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQGCPConfig(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	client, err := mqs.NewGCPSubscriberClient(ctx)
	require.NoError(t, err)
	api := &countingGCPAPI{GCPSubscriberAPI: client}
	path := fmt.Sprintf("projects/%s/subscriptions/%s", config.GCPPubSub.ProjectID, config.GCPPubSub.SubscriptionID)
	subscription := mqs.NewGCPPullSubscription(ctx, api, path, mqs.WithConcurrency(1000), mqs.WithMaxBytes(1<<20))

	const idle = 5 * time.Second
	time.Sleep(idle)
	pulls := api.pulls.Load()
	t.Logf("%d pull requests in %s of idle", pulls, idle)
	assert.LessOrEqual(t, pulls, int64(idle/(250*time.Millisecond))+1)

	published := time.Now()
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "after-idle"}))
	msg, err := subscription.Receive(ctx)
	require.NoError(t, err)
	msg.Ack()
	assert.Less(t, time.Since(published), 2*time.Second)

	stopping := time.Now()
	require.NoError(t, subscription.Shutdown(ctx))
	assert.Less(t, time.Since(stopping), 2*time.Second)
}

// testMQMaxBytesShutdownWhileWaiting stops a consumer while a received
// message waits for bytes. The consumer must stop without handling that
// message. With redelivered, the message must also come back on a new
// subscription well before the queue's visibility timeout (30s): the nack
// sent when the consumer stops has reached the broker.
func testMQMaxBytesShutdownWhileWaiting(t *testing.T, config mqs.QueueConfig, redelivered bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	// One byte: every message is over the limit and runs alone.
	subscription, err := queue.Subscribe(ctx, mqs.WithConcurrency(5), mqs.WithMaxBytes(1))
	require.NoError(t, err)

	started := make(chan string, 2)
	release := make(chan struct{})
	handler := handlerFunc(func(_ context.Context, msg *mqs.Message) error {
		parsed := &Msg{}
		if err := parsed.FromMessage(msg); err != nil {
			msg.Nack()
			return err
		}
		started <- parsed.ID
		<-release
		msg.Ack()
		return nil
	})

	require.NoError(t, queue.Publish(ctx, &Msg{ID: "a"}))
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "b"}))

	runErr := make(chan error, 1)
	go func() {
		runErr <- consumer.New(subscription, handler, consumer.WithConcurrency(5)).Run(ctx)
	}()

	var first string
	select {
	case first = <-started:
	case <-time.After(20 * time.Second):
		t.Fatal("no message handled")
	}
	if mqs.IsByteLimited(subscription) {
		require.Eventually(t, func() bool { return mqs.ByteLimitWaiting(subscription) },
			20*time.Second, 10*time.Millisecond, "second message never waited for bytes")
	} else {
		// The second message is not received while the first holds the limit.
		time.Sleep(time.Second)
	}
	require.Empty(t, started, "second message started while the first holds the limit")

	cancel()
	if redelivered {
		// The nack is sent in the background and the consumer's shutdown
		// does not wait for it, so it only lands while a handler is still
		// in flight. Without this the message comes back after the
		// visibility timeout instead.
		time.Sleep(time.Second)
	}
	close(release)
	select {
	case err := <-runErr:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("consumer did not stop")
	}
	require.Empty(t, started, "waiting message was handled during shutdown")
	if !redelivered {
		return
	}

	recvCtx, recvCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer recvCancel()
	resubscription, err := queue.Subscribe(recvCtx)
	require.NoError(t, err)
	defer resubscription.Shutdown(context.Background())
	msg, err := resubscription.Receive(recvCtx)
	require.NoError(t, err, "waiting message was not returned to the queue")
	parsed := &Msg{}
	require.NoError(t, parsed.FromMessage(msg))
	msg.Ack()
	assert.NotEqual(t, first, parsed.ID)
}

// maxBytesTracker records what the handlers hold. A handler joins after
// Receive returns and leaves before it settles, so it never holds more than
// the subscription counts as in flight.
type maxBytesTracker struct {
	limit int

	mu         sync.Mutex
	bytes      int
	count      int
	maxBytes   int
	maxCount   int
	violations []string

	fullAt int
	full   chan struct{} // closed once fullAt messages are held at once
}

func (tr *maxBytesTracker) start(size int) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.bytes += size
	tr.count++
	if tr.bytes > tr.limit && tr.count > 1 {
		tr.violations = append(tr.violations, fmt.Sprintf("%d bytes across %d messages", tr.bytes, tr.count))
	}
	if tr.count > 1 {
		tr.maxBytes = max(tr.maxBytes, tr.bytes)
	}
	tr.maxCount = max(tr.maxCount, tr.count)
	if tr.count == tr.fullAt {
		select {
		case <-tr.full:
		default:
			close(tr.full)
		}
	}
}

func (tr *maxBytesTracker) finish(size int) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.bytes -= size
	tr.count--
}

type maxBytesCase struct {
	enforced   bool // the limit is observable against this test broker
	rejectable bool
	// batched: the queue is asked for as many messages as fit before their
	// sizes are known, so a message larger than the limit can arrive together
	// with others.
	batched bool
}

// testMQMaxBytes consumes a backlog under a limit of 3.5 messages.
//
// It starts with small messages only, and their handlers hold them until
// three are held at once: the limit lets exactly that many through. The rest
// is published then, including one message larger than the whole limit, and
// handlers become slow instead. One message is nacked on its first delivery.
func testMQMaxBytes(t *testing.T, config mqs.QueueConfig, tc maxBytesCase) {
	const (
		smallCount  = 12
		fitting     = 3
		handlerTime = 200 * time.Millisecond
		nackedID    = "small-00"
	)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	newMsg := func(id string, padding int) *Msg {
		return &Msg{ID: id, Data: map[string]string{"pad": strings.Repeat("x", padding)}}
	}
	bodySize := func(msg *Msg) int {
		m, err := msg.ToMessage()
		require.NoError(t, err)
		return len(m.Body)
	}

	var first, rest []*Msg
	for i := range smallCount {
		msg := newMsg(fmt.Sprintf("small-%02d", i), 4096)
		if i < smallCount/2 {
			first = append(first, msg)
		} else {
			rest = append(rest, msg)
		}
	}
	oversized := newMsg("oversized", 5*4096)
	rest = append([]*Msg{oversized}, rest...)
	total := len(first) + len(rest)

	limit := bodySize(first[0]) * 7 / 2
	require.Greater(t, bodySize(oversized), limit)

	subscription, err := queue.Subscribe(ctx, mqs.WithConcurrency(total), mqs.WithMaxBytes(int64(limit)))
	require.NoError(t, err)
	defer subscription.Shutdown(context.Background())

	for _, msg := range first {
		require.NoError(t, queue.Publish(ctx, msg))
	}

	tracker := &maxBytesTracker{limit: limit, fullAt: fitting, full: make(chan struct{})}
	recvCtx, stopReceiving := context.WithCancel(ctx)
	defer stopReceiving()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-tracker.full:
		case <-ctx.Done():
			return
		}
		for _, msg := range rest {
			if !assert.NoError(t, queue.Publish(ctx, msg)) {
				stopReceiving()
				return
			}
		}
	}()

	var (
		mu     sync.Mutex
		acked  = map[string]int{}
		nacked bool
	)
	for {
		msg, err := subscription.Receive(recvCtx)
		if err != nil {
			if recvCtx.Err() == nil {
				assert.NoError(t, err)
			}
			break
		}
		assert.Equal(t, tc.rejectable, msg.Rejectable())
		size := len(msg.Body)
		tracker.start(size)

		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-tracker.full:
			case <-ctx.Done():
			}
			time.Sleep(handlerTime)

			parsed := &Msg{}
			if !assert.NoError(t, parsed.FromMessage(msg)) {
				tracker.finish(size)
				msg.Nack()
				return
			}

			mu.Lock()
			nack := parsed.ID == nackedID && !nacked
			if nack {
				nacked = true
			} else {
				acked[parsed.ID]++
				if len(acked) == total {
					stopReceiving()
				}
			}
			mu.Unlock()

			tracker.finish(size)
			if nack {
				msg.Nack()
				return
			}
			msg.Ack()
		}()
	}
	wg.Wait()

	require.NoError(t, ctx.Err(), "timed out with %d of %d messages processed, at most %d held at once", len(acked), total, tracker.maxCount)
	for _, msg := range append(first, rest...) {
		assert.GreaterOrEqual(t, acked[msg.ID], 1, "message %s not processed", msg.ID)
	}
	assert.True(t, nacked)
	if !tc.enforced {
		return
	}
	if !tc.batched {
		assert.Empty(t, tracker.violations, "bytes being handled exceeded the limit of %d", limit)
		assert.LessOrEqual(t, tracker.maxBytes, limit)
	}
	assert.Equal(t, fitting, tracker.maxCount, "messages within the limit should run concurrently, and no more")
}
