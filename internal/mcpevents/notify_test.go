package mcpevents

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestNotifier(t *testing.T, mutate func(*NotifierConfig)) *Notifier {
	t.Helper()
	cfg := NotifierConfig{Client: guardedLikeClient(t)}
	if mutate != nil {
		mutate(&cfg)
	}
	n, err := NewNotifier(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = n.Close() })
	return n
}

func testTermination(url string, key []byte) Termination {
	return Termination{
		TenantID:       "tenant_1",
		SubscriptionID: "sub_3f1c8e2b0d49f7e6a1b2c3d4e5f60718",
		URL:            url,
		Secrets:        []Secret{{Key: key}},
		CreatedAt:      time.UnixMilli(1791576000123),
		Error:          AccessRevoked(),
	}
}

func TestNotifier_Sends(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	_, previous := secretOf(24, base64.StdEncoding)
	rc := newReceiver(t, previous, nil) // knows only the previous secret: dual signing matters
	n := newTestNotifier(t, nil)

	term := testTermination(rc.URL+"/hook", key)
	future := time.Now().Add(time.Hour)
	term.Secrets = append(term.Secrets, Secret{Key: previous, InvalidAt: &future})
	require.True(t, n.Enqueue(term))
	require.NoError(t, n.Close())

	reqs := rc.Requests()
	require.Len(t, reqs, 1)
	r := reqs[0]
	assert.True(t, r.Verified)
	assert.Equal(t, `{"type":"terminated","error":{"code":-32012,"message":"Forbidden","data":{"reason":"access_revoked"}}}`, string(r.Body))
	assert.Equal(t, TerminatedMessageID(term.SubscriptionID, 1791576000123), r.Header.Get("webhook-id"))
	assert.Equal(t, term.SubscriptionID, r.Header.Get("X-MCP-Subscription-Id"))
	assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
	assert.Equal(t, 2, strings.Count(r.Header.Get("webhook-signature"), "v1,"))
	assert.NotEmpty(t, r.Header.Get("webhook-timestamp"))
	assert.Equal(t, NotifierStats{Sent: 1}, n.Stats())
}

// A terminated envelope spells its headers as deliveries do on the wire.
func TestNotifier_HeaderSpellingOnTheWire(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	url, got := newRawReceiver(t, nil)
	n := newTestNotifier(t, nil)

	term := testTermination(url+"/hook", key)
	require.True(t, n.Enqueue(term))
	req := receiveRaw(t, got)
	require.NoError(t, n.Close())

	assert.Equal(t, []string{"Content-Type", "webhook-id", "webhook-timestamp", "webhook-signature", "X-MCP-Subscription-Id"},
		sortedLike(req.deliveryHeaderNames()))
	assert.Equal(t, term.SubscriptionID, req.header.Get("X-MCP-Subscription-Id"))
	verifyWith(t, key, req.body, req.header, true)
}

func TestNotifier_ProfileAndRotationEnd(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	_, previous := secretOf(24, base64.StdEncoding)
	rc := newReceiver(t, key, nil)
	n := newTestNotifier(t, func(c *NotifierConfig) { c.Profile = CodeProfileSEP3415 })

	term := testTermination(rc.URL, key)
	past := time.Now().Add(-time.Second)
	term.Secrets = append(term.Secrets, Secret{Key: previous, InvalidAt: &past})
	term.Error = SchemaChanged()
	require.NoError(t, n.EnqueueWait(context.Background(), term))
	require.NoError(t, n.Close())

	reqs := rc.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, `{"type":"terminated","error":{"code":-32026,"message":"Unsupported","data":{"feature":"payloadSchema","reason":"schema_changed"}}}`, string(reqs[0].Body))
	assert.Equal(t, 1, strings.Count(reqs[0].Header.Get("webhook-signature"), "v1,"), "the previous secret stopped signing")
	assert.True(t, reqs[0].Verified)
}

func TestNotifier_Failures(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	refused := newReceiver(t, key, func(w http.ResponseWriter, r receivedRequest) { w.WriteHeader(http.StatusServiceUnavailable) })
	redirect := newReceiver(t, key, func(w http.ResponseWriter, r receivedRequest) {
		w.Header().Set("Location", refused.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	// Even a client that would follow redirects doesn't.
	n := newTestNotifier(t, func(c *NotifierConfig) { c.Client = &http.Client{} })

	require.True(t, n.Enqueue(testTermination(refused.URL, key)))
	require.True(t, n.Enqueue(testTermination(redirect.URL, key)))
	unsigned := testTermination(refused.URL, key)
	past := time.Now().Add(-time.Second)
	unsigned.Secrets = []Secret{{Key: key, InvalidAt: &past}}
	require.True(t, n.Enqueue(unsigned))
	badURL := testTermination("ftp://a.example/", key)
	require.True(t, n.Enqueue(badURL))
	require.NoError(t, n.Close())

	assert.Len(t, refused.Requests(), 1, "sent once, never retried, redirect not followed")
	assert.Len(t, redirect.Requests(), 1)
	assert.Equal(t, NotifierStats{Failed: 4}, n.Stats())
}

func TestNotifier_Invalid(t *testing.T) {
	t.Parallel()
	n := newTestNotifier(t, nil)
	for _, mutate := range []func(*Termination){
		func(t *Termination) { t.URL = "" },
		func(t *Termination) { t.SubscriptionID = "" },
		func(t *Termination) { t.SubscriptionID = "sub\r\nX-Injected: 1" },
		func(t *Termination) { t.Error = nil },
	} {
		term := testTermination("https://a.example/", []byte("k"))
		mutate(&term)
		assert.False(t, n.Enqueue(term))
		assert.ErrorIs(t, n.EnqueueWait(context.Background(), term), ErrInvalidTermination)
	}
	assert.Zero(t, n.Stats())
}

func TestNotifier_QueueSaturation(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	rc := newReceiver(t, key, func(w http.ResponseWriter, r receivedRequest) {
		entered <- struct{}{}
		<-release
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	n := newTestNotifier(t, func(c *NotifierConfig) {
		c.QueueSize = 2
		c.Workers = 1
	})

	// The worker takes the first and blocks on the slow receiver.
	require.True(t, n.Enqueue(testTermination(rc.URL+"/0", key)))
	<-entered
	// Two fit in the queue; the rest are dropped without blocking.
	require.True(t, n.Enqueue(testTermination(rc.URL+"/1", key)))
	require.True(t, n.Enqueue(testTermination(rc.URL+"/2", key)))
	start := time.Now()
	for i := 3; i < 10; i++ {
		assert.False(t, n.Enqueue(testTermination(rc.URL, key)))
	}
	assert.Less(t, time.Since(start), 100*time.Millisecond, "Enqueue never blocks")
	assert.Equal(t, uint64(7), n.Stats().Dropped)

	// EnqueueWait blocks while full and gives up with its ctx.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, n.EnqueueWait(ctx, testTermination(rc.URL, key)), context.DeadlineExceeded)

	// It goes through once there is room.
	done := make(chan error, 1)
	go func() { done <- n.EnqueueWait(context.Background(), testTermination(rc.URL+"/3", key)) }()
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, n.Close())
	assert.Equal(t, NotifierStats{Sent: 4, Dropped: 7}, n.Stats())
	assert.Len(t, rc.Requests(), 4)
}

func TestNotifier_EnqueueWaitPacesProducers(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	rc := newReceiver(t, key, nil)
	n := newTestNotifier(t, func(c *NotifierConfig) {
		c.QueueSize = 1
		c.Workers = 2
	})
	for range 50 {
		require.NoError(t, n.EnqueueWait(context.Background(), testTermination(rc.URL, key)))
	}
	require.NoError(t, n.Close())
	assert.Equal(t, NotifierStats{Sent: 50}, n.Stats())
}

func TestNotifier_CloseDrains(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	rc := newReceiver(t, key, func(w http.ResponseWriter, r receivedRequest) { time.Sleep(20 * time.Millisecond) })
	n := newTestNotifier(t, func(c *NotifierConfig) { c.Workers = 2 })
	for range 10 {
		require.True(t, n.Enqueue(testTermination(rc.URL, key)))
	}
	require.NoError(t, n.Close())
	assert.Equal(t, NotifierStats{Sent: 10}, n.Stats(), "everything queued before Close is sent")

	// Closed: nothing more is accepted.
	assert.False(t, n.Enqueue(testTermination(rc.URL, key)))
	assert.ErrorIs(t, n.EnqueueWait(context.Background(), testTermination(rc.URL, key)), ErrQueueClosed)
	require.NoError(t, n.Close(), "idempotent")
}

func TestNotifier_CloseGivesUpAfterDrainTimeout(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	release := make(chan struct{})
	rc := newReceiver(t, key, func(w http.ResponseWriter, r receivedRequest) { <-release })
	t.Cleanup(func() { close(release) })
	n := newTestNotifier(t, func(c *NotifierConfig) {
		c.Workers = 1
		c.DrainTimeout = 100 * time.Millisecond
	})
	for range 5 {
		require.True(t, n.Enqueue(testTermination(rc.URL, key)))
	}
	start := time.Now()
	require.NoError(t, n.Close())
	assert.Less(t, time.Since(start), 2*time.Second)
	stats := n.Stats()
	assert.Equal(t, uint64(1), stats.Failed, "the in-flight send is aborted")
	assert.Equal(t, uint64(4), stats.Dropped)
	assert.Zero(t, stats.Sent)
}

func TestNotifier_BlockedProducerReleasedByClose(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	rc := newReceiver(t, key, func(w http.ResponseWriter, r receivedRequest) {
		entered <- struct{}{}
		<-release
	})
	t.Cleanup(func() { close(release) })
	n := newTestNotifier(t, func(c *NotifierConfig) {
		c.QueueSize = 1
		c.Workers = 1
		c.DrainTimeout = 50 * time.Millisecond
	})
	require.True(t, n.Enqueue(testTermination(rc.URL, key)))
	<-entered
	require.True(t, n.Enqueue(testTermination(rc.URL, key)))
	done := make(chan error, 1)
	go func() { done <- n.EnqueueWait(context.Background(), testTermination(rc.URL, key)) }()
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, n.Close())
	assert.ErrorIs(t, <-done, ErrQueueClosed)
}

func TestNotifier_HostLimiter(t *testing.T) {
	t.Parallel()
	_, key := secretOf(32, base64.StdEncoding)
	rc := newReceiver(t, key, nil)

	t.Run("slots are taken and returned", func(t *testing.T) {
		limiter := newCountingLimiter(8)
		n := newTestNotifier(t, func(c *NotifierConfig) { c.HostLimiter = limiter })
		for range 5 {
			require.True(t, n.Enqueue(testTermination(rc.URL, key)))
		}
		require.NoError(t, n.Close())
		acquired, released, _ := limiter.counts()
		assert.Equal(t, 5, acquired)
		assert.Equal(t, 5, released)
		assert.Equal(t, uint64(5), n.Stats().Sent)
	})

	t.Run("a host that stays full is deferred, then dropped", func(t *testing.T) {
		before := len(rc.Requests())
		limiter := newCountingLimiter(0)
		n := newTestNotifier(t, func(c *NotifierConfig) { c.HostLimiter = limiter })
		n.hostWait = 5 * time.Millisecond
		require.True(t, n.Enqueue(testTermination(rc.URL, key)))
		require.Eventually(t, func() bool { return n.Stats().Dropped == 1 }, 5*time.Second, 5*time.Millisecond)
		_, _, denied := limiter.counts()
		assert.GreaterOrEqual(t, denied, maxDeferrals+1)
		require.NoError(t, n.Close())
		assert.Len(t, rc.Requests(), before)
	})

	t.Run("a full host doesn't hold back others", func(t *testing.T) {
		other := newReceiver(t, key, nil)
		limiter := &selectiveLimiter{deny: HostPort(mustURL(t, rc.URL)), inner: newCountingLimiter(8)}
		n := newTestNotifier(t, func(c *NotifierConfig) {
			c.HostLimiter = limiter
			c.Workers = 1
		})
		n.hostWait = 20 * time.Millisecond
		require.True(t, n.Enqueue(testTermination(rc.URL, key)))
		require.True(t, n.Enqueue(testTermination(other.URL, key)))
		require.Eventually(t, func() bool { return len(other.Requests()) == 1 }, 5*time.Second, 5*time.Millisecond)
		require.NoError(t, n.Close())
	})
}

// selectiveLimiter denies one host and defers to inner for the rest.
type selectiveLimiter struct {
	deny  string
	inner HostLimiter
}

func (l *selectiveLimiter) Acquire(ctx context.Context, hostport string) (func(), error) {
	if hostport == l.deny {
		<-ctx.Done()
		return func() {}, ctx.Err()
	}
	return l.inner.Acquire(ctx, hostport)
}

func TestNotifier_Disabled(t *testing.T) {
	leakCheck(t)
	n, err := NewNotifier(NotifierConfig{Disabled: true})
	require.NoError(t, err)
	assert.False(t, n.Enabled())
	assert.True(t, n.Enqueue(testTermination("https://a.example/", []byte("k"))), "a no-op, not a drop: MCP_SEND_TERMINATED=false is intended")
	assert.True(t, n.Enqueue(Termination{}), "nothing is checked when nothing is sent")
	assert.NoError(t, n.EnqueueWait(context.Background(), testTermination("https://a.example/", []byte("k"))))
	assert.Zero(t, n.Stats())
	assert.NoError(t, n.Close())

	_, err = NewNotifier(NotifierConfig{})
	assert.Error(t, err, "a client is required when enabled")
}

func TestNotifier_NoGoroutineLeak(t *testing.T) {
	leakCheck(t)
	_, key := secretOf(32, base64.StdEncoding)
	rc := newReceiver(t, key, nil)
	client := guardedLikeClient(t)
	n, err := NewNotifier(NotifierConfig{Client: client, Workers: 4})
	require.NoError(t, err)
	assert.True(t, n.Enabled())
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				_ = n.EnqueueWait(context.Background(), testTermination(rc.URL, key))
			}
		}()
	}
	wg.Wait()
	require.NoError(t, n.Close())
	assert.Equal(t, uint64(20), n.Stats().Sent)
	client.CloseIdleConnections()
	rc.Close()
}
