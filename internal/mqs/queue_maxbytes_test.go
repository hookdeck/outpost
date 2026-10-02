package mqs_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	nativepubsub "cloud.google.com/go/pubsub"
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
	testMQMaxBytes(t, testinfra.NewMQGCPConfig(t, nil), maxBytesCase{enforced: true})
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
	// Redelivery is not checked. The Pub/Sub client nacks the waiting message
	// when the consumer stops, but its stream stays open until the handler in
	// flight has settled, and the emulator sends the nacked message straight
	// back to that stream, where nobody reads it: it returns once the
	// stream's 60s ack deadline passes.
	testMQMaxBytesShutdownWhileWaiting(t, testinfra.NewMQGCPConfig(t, nil), false)
}

// On Pub/Sub the limit is the client's own MaxOutstandingBytes. Without a
// limit the receive settings are the ones used before the option existed.
func TestGCPPubSubQueue_MaxBytesSetsMaxOutstandingBytes(t *testing.T) {
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

	want.MaxOutstandingBytes = 1 << 20
	assert.Equal(t, want, mqs.GCPReceiveSettings(queue, mqs.WithConcurrency(5), mqs.WithMaxBytes(1<<20)))
}

// The Pub/Sub client enforces the limit, so the subscription is never wrapped
// by the shared helper, and it manages its own concurrency either way.
func TestIntegrationMQMaxBytes_GCPPubSubNotWrapped(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQGCPConfig(t, nil)
	ctx := context.Background()
	queue := mqs.NewQueue(&config)

	cases := []struct {
		name string
		opts []mqs.SubscribeOption
	}{
		{name: "no option", opts: []mqs.SubscribeOption{mqs.WithConcurrency(5)}},
		{name: "zero", opts: []mqs.SubscribeOption{mqs.WithConcurrency(5), mqs.WithMaxBytes(0)}},
		{name: "limit", opts: []mqs.SubscribeOption{mqs.WithConcurrency(5), mqs.WithMaxBytes(1 << 20)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subscription, err := queue.Subscribe(ctx, tc.opts...)
			require.NoError(t, err)
			defer subscription.Shutdown(ctx)

			assert.False(t, mqs.IsByteLimited(subscription))
			concurrent, ok := subscription.(mqs.ConcurrentSubscription)
			require.True(t, ok)
			assert.True(t, concurrent.SupportsConcurrency())
		})
	}
}

// With a byte limit the count limit is held by the Pub/Sub client as well:
// the emulator sends the whole backlog at once whatever the stream asks for,
// and only two messages reach handlers at a time.
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
	for i := range total {
		require.NoError(t, queue.Publish(ctx, &Msg{ID: fmt.Sprintf("m-%02d", i)}))
	}

	subscription, err := queue.Subscribe(ctx, mqs.WithConcurrency(2), mqs.WithMaxBytes(1<<30))
	require.NoError(t, err)
	defer subscription.Shutdown(context.Background())

	var (
		mu       sync.Mutex
		inFlight int
		maxSeen  int
		wg       sync.WaitGroup
	)
	for range total {
		msg, err := subscription.Receive(ctx)
		require.NoError(t, err)
		mu.Lock()
		inFlight++
		maxSeen = max(maxSeen, inFlight)
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(200 * time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
			msg.Ack()
		}()
	}
	wg.Wait()
	assert.Equal(t, 2, maxSeen)
}

// The emulator pushes a backlog many times the limit at the subscriber. The
// process must hold the limit plus what the client has already read from the
// stream, not the backlog.
func TestIntegrationMQMaxBytes_GCPPubSubBacklogStaysOutOfProcess(t *testing.T) {
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
		// The backlog is 64 MiB. Measured: about 14 MiB over the baseline.
		maxGrowth = 32 << 20
	)
	padding := strings.Repeat("x", bodySize)
	for i := range total {
		require.NoError(t, queue.Publish(ctx, &Msg{ID: fmt.Sprintf("m-%02d", i), Data: map[string]string{"pad": padding}}))
	}
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

	held := make(chan *mqs.Message, total)
	go func() {
		for {
			msg, err := subscription.Receive(ctx)
			if err != nil {
				return
			}
			held <- msg
		}
	}()

	require.Eventually(t, func() bool { return len(held) == fitting }, 30*time.Second, 10*time.Millisecond)
	// Time for the client to read whatever it is going to read.
	time.Sleep(3 * time.Second)
	assert.Len(t, held, fitting)
	growth := int64(heap()) - int64(baseline)
	assert.Less(t, growth, int64(maxGrowth), "heap grew by %d MiB with a %d MiB limit", growth>>20, limit>>20)

	for range total {
		select {
		case msg := <-held:
			msg.Ack()
		case <-ctx.Done():
			t.Fatal("backlog not consumed")
		}
	}
}

// A message that is never settled must not keep the subscription from
// shutting down.
func TestIntegrationMQMaxBytes_GCPPubSubShutdownWithUnsettledMessage(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQGCPConfig(t, nil)
	ctx := context.Background()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	subscription, err := queue.Subscribe(ctx, mqs.WithConcurrency(5), mqs.WithMaxBytes(1<<20))
	require.NoError(t, err)
	require.NoError(t, queue.Publish(ctx, &Msg{ID: "unsettled"}))

	recvCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err = subscription.Receive(recvCtx)
	require.NoError(t, err)

	stopped := make(chan error, 1)
	go func() { stopped <- subscription.Shutdown(ctx) }()
	select {
	case <-stopped:
	// The client waits for its next lease pass (up to 10s here) before it
	// gives up on the unsettled message.
	case <-time.After(30 * time.Second):
		t.Fatal("shutdown did not return")
	}
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
		// The wait happens inside the queue's own client, out of sight.
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
	assert.Empty(t, tracker.violations, "bytes being handled exceeded the limit of %d", limit)
	assert.LessOrEqual(t, tracker.maxBytes, limit)
	assert.Equal(t, fitting, tracker.maxCount, "messages within the limit should run concurrently, and no more")
}
