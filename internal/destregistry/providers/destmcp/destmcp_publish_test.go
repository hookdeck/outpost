package destmcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testUserAgent = "Outpost/test"

func testEvent() *models.Event {
	return &models.Event{
		ID:       "evt_01J9ZK4Q0V",
		TenantID: testTenantID,
		Topic:    topicOrderCreated,
		Time:     time.Date(2026, 10, 9, 16, 58, 12, 0, time.UTC),
		Metadata: map[string]string{"ignored": "yes"},
		Data:     json.RawMessage(`{"order_id":"ord_42","total":180,"currency":"USD"}`),
	}
}

// localProvider delivers to loopback receivers.
func localProvider(t *testing.T, opts ...providerOption) *destmcp.Provider {
	t.Helper()
	guard := loopbackGuard(t)
	base := []providerOption{func(c *destmcp.Config) {
		c.Guard = guard
		c.Client = newClient(t, guard, destregistry.HTTPClientConfig{})
		c.UserAgent = testUserAgent
	}}
	return newProvider(t, append(base, opts...)...)
}

// localSubscription is a subscription delivering to callbackURL.
func localSubscription(t *testing.T, _ *destmcp.Provider, callbackURL string, mutate ...func(*models.Destination)) *models.Destination {
	t.Helper()
	_, normalized, err := mcpevents.NormalizeCallbackURL(callbackURL)
	require.NoError(t, err)
	d := subscriptionFor(testPrincipal, normalized, "{}", testSecret(1))
	for _, m := range mutate {
		m(d)
	}
	return d
}

func publish(t *testing.T, p *destmcp.Provider, d *models.Destination, event *models.Event) (*destregistry.Delivery, error) {
	t.Helper()
	pub, err := p.CreatePublisher(context.Background(), d)
	require.NoError(t, err)
	defer pub.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return pub.Publish(ctx, event)
}

type capturedRequest struct {
	Header http.Header
	Body   []byte
}

// recorder is an httptest receiver that records requests and answers with
// respond (200 when nil).
type recorder struct {
	*httptest.Server
	mu       sync.Mutex
	requests []capturedRequest
}

func newRecorder(t *testing.T, respond http.HandlerFunc) *recorder {
	t.Helper()
	rec := &recorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.requests = append(rec.requests, capturedRequest{Header: r.Header.Clone(), Body: body})
		rec.mu.Unlock()
		if respond != nil {
			respond(w, r)
		}
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (r *recorder) Requests() []capturedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]capturedRequest(nil), r.requests...)
}

func requirePublishErr(t *testing.T, err error) *destregistry.ErrDestinationPublishAttempt {
	t.Helper()
	var pubErr *destregistry.ErrDestinationPublishAttempt
	require.ErrorAs(t, err, &pubErr)
	assert.Equal(t, destmcp.Type, pubErr.Provider)
	return pubErr
}

// The request carries exactly the documented headers, spelled as
// documented, and the envelope as its body.
func TestPublish_RequestOnTheWire(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	type rawRequest struct {
		requestLine string
		headerNames []string
		header      http.Header
		body        []byte
	}
	got := make(chan rawRequest, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := textproto.NewReader(bufio.NewReader(conn))
		var req rawRequest
		req.requestLine, _ = reader.ReadLine()
		req.header = http.Header{}
		for {
			line, err := reader.ReadLine()
			if err != nil || line == "" {
				break
			}
			name, value, _ := strings.Cut(line, ":")
			req.headerNames = append(req.headerNames, name)
			req.header.Add(name, strings.TrimSpace(value))
		}
		n, _ := strconv.Atoi(req.header.Get("Content-Length"))
		req.body = make([]byte, n)
		_, _ = io.ReadFull(reader.R, req.body)
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		got <- req
	}()

	now := time.Now()
	p := localProvider(t)
	destmcp.SetNow(p, func() time.Time { return now })
	d := localSubscription(t, p, "http://"+ln.Addr().String()+"/mcp-events/abc123?x=1")
	event := testEvent()

	delivery, err := publish(t, p, d, event)
	require.NoError(t, err)
	assert.Equal(t, models.AttemptStatusSuccess, delivery.Status)
	assert.Equal(t, "200", delivery.Code)

	req := <-got
	assert.Equal(t, "POST /mcp-events/abc123?x=1 HTTP/1.1", req.requestLine)
	assert.ElementsMatch(t, []string{
		"Host", "User-Agent", "Content-Length",
		"Content-Type", "webhook-id", "webhook-timestamp", "webhook-signature", "X-MCP-Subscription-Id",
	}, req.headerNames, "exactly the documented headers, in the documented spelling")
	assert.Equal(t, "application/json", req.header.Get("Content-Type"))
	assert.Equal(t, mcpevents.DeliveryMessageID(d.TenantID, d.ID, event.ID), req.header.Get("webhook-id"))
	assert.Equal(t, strconv.FormatInt(now.Unix(), 10), req.header.Get("webhook-timestamp"))
	assert.Equal(t, d.ID, req.header.Get("X-MCP-Subscription-Id"))
	assert.Equal(t, testUserAgent, req.header.Get("User-Agent"))

	envelope, err := mcpevents.EventEnvelope(event)
	require.NoError(t, err)
	assert.Equal(t, string(envelope), string(req.body), "the body is the envelope, byte for byte")
	assert.JSONEq(t, `{"eventId":"evt_01J9ZK4Q0V","name":"order.created","timestamp":"2026-10-09T16:58:12Z","data":{"order_id":"ord_42","total":180,"currency":"USD"},"cursor":null}`, string(req.body))

	wh, err := standardwebhooks.NewWebhook(testSecret(1))
	require.NoError(t, err)
	assert.NoError(t, wh.Verify(req.body, req.header), "Standard Webhooks libraries verify it unchanged")
}

func TestPublish_Signatures(t *testing.T) {
	t.Parallel()

	t.Run("single secret", func(t *testing.T) {
		t.Parallel()
		rec := newRecorder(t, nil)
		p := localProvider(t)
		_, err := publish(t, p, localSubscription(t, p, rec.URL+"/hook"), testEvent())
		require.NoError(t, err)

		reqs := rec.Requests()
		require.Len(t, reqs, 1)
		assert.Len(t, strings.Fields(reqs[0].Header.Get("webhook-signature")), 1)
		wh, err := standardwebhooks.NewWebhook(testSecret(1))
		require.NoError(t, err)
		assert.NoError(t, wh.Verify(reqs[0].Body, reqs[0].Header))
		other, err := standardwebhooks.NewWebhook(testSecret(2))
		require.NoError(t, err)
		assert.Error(t, other.Verify(reqs[0].Body, reqs[0].Header))
	})

	t.Run("both secrets during rotation", func(t *testing.T) {
		t.Parallel()
		rec := newRecorder(t, nil)
		p := localProvider(t)
		d := localSubscription(t, p, rec.URL+"/hook", func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecret] = testSecret(2)
			d.Credentials[destmcp.CredentialPreviousSecretInvalidAt] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		})
		_, err := publish(t, p, d, testEvent())
		require.NoError(t, err)

		reqs := rec.Requests()
		require.Len(t, reqs, 1)
		sigs := strings.Fields(reqs[0].Header.Get("webhook-signature"))
		require.Len(t, sigs, 2, "current first, then previous")
		for i, secret := range []string{testSecret(1), testSecret(2)} {
			wh, err := standardwebhooks.NewWebhook(secret)
			require.NoError(t, err)
			assert.NoError(t, wh.Verify(reqs[0].Body, reqs[0].Header), "secret %d", i)
			expected, err := wh.Sign(reqs[0].Header.Get("webhook-id"), time.Unix(mustInt(t, reqs[0].Header.Get("webhook-timestamp")), 0), reqs[0].Body)
			require.NoError(t, err)
			assert.Equal(t, expected, sigs[i])
		}
	})

	t.Run("previous secret stops signing at its expiry", func(t *testing.T) {
		t.Parallel()
		rec := newRecorder(t, nil)
		p := localProvider(t)
		d := localSubscription(t, p, rec.URL+"/hook", func(d *models.Destination) {
			d.Credentials[destmcp.CredentialPreviousSecret] = testSecret(2)
			d.Credentials[destmcp.CredentialPreviousSecretInvalidAt] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
		})
		_, err := publish(t, p, d, testEvent())
		require.NoError(t, err)

		reqs := rec.Requests()
		require.Len(t, reqs, 1)
		assert.Len(t, strings.Fields(reqs[0].Header.Get("webhook-signature")), 1)
		old, err := standardwebhooks.NewWebhook(testSecret(2))
		require.NoError(t, err)
		assert.Error(t, old.Verify(reqs[0].Body, reqs[0].Header))
	})
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	require.NoError(t, err)
	return n
}

// Bodies past the stored part are drained (up to a bound), so the
// connection goes back to the pool.
func TestPublish_ConnectionReuse(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		size   int
		reused bool
	}{
		"drained": {size: destmcp.DefaultMaxResponseBodyBytes + 1000, reused: true},
		// Past the drain bound and past what net/http drains on Close
		// (256 KiB), the connection is dropped instead.
		"too large to drain": {size: destmcp.DefaultMaxResponseBodyBytes + destmcp.MaxDrainBytes + (300 << 10), reused: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := strings.Repeat("b", tc.size)
			rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(len(body)))
				_, _ = io.WriteString(w, body)
			})
			var mu sync.Mutex
			var reused []bool
			guard := loopbackGuard(t)
			client := newClient(t, guard, destregistry.HTTPClientConfig{OnConnection: func(r bool) {
				mu.Lock()
				reused = append(reused, r)
				mu.Unlock()
			}})
			p := localProvider(t, func(c *destmcp.Config) { c.Client = client })
			d := localSubscription(t, p, rec.URL+"/hook")
			for range 2 {
				_, err := publish(t, p, d, testEvent())
				require.NoError(t, err)
			}
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, []bool{false, tc.reused}, reused)
		})
	}
}

// Close returns at once, even with a publish in flight: the publisher holds
// nothing to wait for.
func TestPublish_CloseIsImmediate(t *testing.T) {
	t.Parallel()
	unblock := make(chan struct{})
	entered := make(chan struct{})
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-unblock
	})
	p := localProvider(t)
	pub, err := p.CreatePublisher(context.Background(), localSubscription(t, p, rec.URL+"/hook"))
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, err := pub.Publish(context.Background(), testEvent())
		done <- err
	}()
	<-entered

	closed := make(chan error, 1)
	go func() { closed <- pub.Close() }()
	select {
	case err := <-closed:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close waited for the in-flight publish")
	}
	close(unblock)
	require.NoError(t, <-done)
}

func TestCreatePublisher_InvalidDestination(t *testing.T) {
	t.Parallel()
	p := localProvider(t)
	tests := map[string]func(*models.Destination){
		"bad url":             func(d *models.Destination) { d.Config[destmcp.ConfigURL] = "nope" },
		"bad secret":          func(d *models.Destination) { d.Credentials[destmcp.CredentialSecret] = "nope" },
		"bad previous secret": func(d *models.Destination) { d.Credentials[destmcp.CredentialPreviousSecret] = "nope" },
		"bad subscription id": func(d *models.Destination) { d.ID = "sub 1\r\n" },
		"missing id":          func(d *models.Destination) { d.ID = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := localSubscription(t, p, "http://127.0.0.1:1/hook", mutate)
			_, err := p.CreatePublisher(context.Background(), d)
			pubErr := requirePublishErr(t, err)
			assert.True(t, pubErr.NonRetryable)
			assert.NotContains(t, err.Error(), testSecret(1))
		})
	}
}

// CreatePublisher does no I/O: it succeeds for a destination whose host
// doesn't resolve.
func TestCreatePublisher_NoIO(t *testing.T) {
	t.Parallel()
	resolver := newResolver()
	p := newProvider(t, func(c *destmcp.Config) { c.Guard = &netguard.Guard{Resolver: resolver} })
	pub, err := p.CreatePublisher(context.Background(), newSubscription(t, func(d *models.Destination) {
		d.Config[destmcp.ConfigURL] = "https://nx.example.com/hook"
	}))
	require.NoError(t, err)
	assert.NoError(t, pub.Close())
	assert.Empty(t, resolver.Calls())
}

func TestPublish_WebhookID(t *testing.T) {
	t.Parallel()
	rec := newRecorder(t, nil)
	p := localProvider(t)
	var now atomic.Int64
	now.Store(time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC).Unix())
	destmcp.SetNow(p, func() time.Time { return time.Unix(now.Load(), 0) })
	d := localSubscription(t, p, rec.URL+"/hook")
	other := localSubscription(t, p, rec.URL+"/hook", func(d *models.Destination) { d.ID = "sub_other" })
	event := testEvent()

	for range 2 {
		_, err := publish(t, p, d, event)
		require.NoError(t, err)
		now.Add(5)
	}
	_, err := publish(t, p, other, event)
	require.NoError(t, err)
	next := testEvent()
	next.ID = "evt_next"
	_, err = publish(t, p, d, next)
	require.NoError(t, err)

	reqs := rec.Requests()
	require.Len(t, reqs, 4)
	id := reqs[0].Header.Get("webhook-id")
	assert.Equal(t, id, reqs[1].Header.Get("webhook-id"), "stable across attempts")
	assert.NotEqual(t, reqs[0].Header.Get("webhook-timestamp"), reqs[1].Header.Get("webhook-timestamp"), "fresh timestamp per attempt")
	assert.NotEqual(t, reqs[0].Header.Get("webhook-signature"), reqs[1].Header.Get("webhook-signature"), "fresh signature per attempt")
	assert.NotEqual(t, id, reqs[2].Header.Get("webhook-id"), "unique per subscription")
	assert.NotEqual(t, id, reqs[3].Header.Get("webhook-id"), "unique per event")
	assert.NotContains(t, id, event.ID)
	assert.Regexp(t, `^[A-Za-z0-9_-]+$`, id)
}
