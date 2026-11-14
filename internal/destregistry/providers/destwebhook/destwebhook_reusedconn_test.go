package destwebhook_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetSecondRequestServer answers every request with 200 except the second
// one it receives: it reads that request in full and then resets the
// connection without responding. That is what a client sees when the
// destination (or a proxy in front of it) has dropped a keep-alive connection
// the client still thinks is open.
type resetSecondRequestServer struct {
	ln net.Listener

	mu       sync.Mutex
	requests []receivedRequest
}

type receivedRequest struct {
	conn   int
	header http.Header
}

func newResetSecondRequestServer(t *testing.T) *resetSecondRequestServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &resetSecondRequestServer{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go s.serve()
	return s
}

func (s *resetSecondRequestServer) URL() string {
	return "http://" + s.ln.Addr().String() + "/webhook"
}

func (s *resetSecondRequestServer) Requests() []receivedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]receivedRequest(nil), s.requests...)
}

func (s *resetSecondRequestServer) serve() {
	for connID := 1; ; connID++ {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn.(*net.TCPConn), connID)
	}
}

func (s *resetSecondRequestServer) handle(conn *net.TCPConn, connID int) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, req.Body)
		req.Body.Close()

		s.mu.Lock()
		s.requests = append(s.requests, receivedRequest{conn: connID, header: req.Header})
		n := len(s.requests)
		s.mu.Unlock()

		if n == 2 {
			// Close with SO_LINGER 0 so the client gets an RST, not a FIN.
			_ = conn.SetLinger(0)
			return
		}
		if _, err := io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"); err != nil {
			return
		}
	}
}

// A delivery written to a reused keep-alive connection that is reset before
// any response should be resent on another connection rather than reported as
// a failed attempt. See https://github.com/hookdeck/outpost/issues/1150
func TestWebhookPublisher_RetriesResetOnReusedConnection(t *testing.T) {
	t.Parallel()

	server := newResetSecondRequestServer(t)
	provider := NewTestProvider(t)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{"url": server.URL()}),
		testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "test-secret"}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })

	ctx := context.Background()

	first := testutil.EventFactory.Any()
	delivery, err := publisher.Publish(ctx, &first)
	require.NoError(t, err)
	require.Equal(t, "success", delivery.Status)

	// Goes out on the pooled connection from the first delivery, which the
	// server resets after reading the request.
	second := testutil.EventFactory.Any()
	delivery, err = publisher.Publish(ctx, &second)
	require.NoError(t, err)
	require.Equal(t, "success", delivery.Status)

	requests := server.Requests()
	require.Len(t, requests, 3)
	assert.Equal(t, 1, requests[0].conn)
	assert.Equal(t, 1, requests[1].conn, "second delivery should reuse the first connection")
	assert.Equal(t, 2, requests[2].conn, "resend should go out on a new connection")
	assert.Equal(t, requests[1].header.Get("x-outpost-event-id"), requests[2].header.Get("x-outpost-event-id"))

	// The replay marker must not reach the destination.
	for i, r := range requests {
		_, ok := r.header["Idempotency-Key"]
		assert.False(t, ok, "request %d carried an Idempotency-Key header", i+1)
	}
}

// A connection that fails on its first use is not retried: the server got a
// fresh connection and dropped it, so a resend would most likely fail the
// same way. The attempt is still recorded as failed.
func TestWebhookPublisher_DoesNotRetryResetOnFreshConnection(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	var mu sync.Mutex
	received := 0
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn *net.TCPConn) {
				defer conn.Close()
				req, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, req.Body)
				mu.Lock()
				received++
				mu.Unlock()
				_ = conn.SetLinger(0)
			}(conn.(*net.TCPConn))
		}
	}()

	provider := NewTestProvider(t)
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{"url": "http://" + ln.Addr().String() + "/webhook"}),
		testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "test-secret"}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })

	event := testutil.EventFactory.Any()
	delivery, err := publisher.Publish(context.Background(), &event)
	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "failed", delivery.Status)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, received)
}

func TestWebhookPublisher_IdempotencyKeyHeader(t *testing.T) {
	t.Parallel()

	format := func(t *testing.T, customHeaders string) *http.Request {
		t.Helper()
		config := map[string]string{"url": "http://example.com/webhook"}
		if customHeaders != "" {
			config["custom_headers"] = customHeaders
		}
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(config),
			testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "test-secret"}),
		)
		publisher, err := NewTestProvider(t).CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)
		t.Cleanup(func() { publisher.Close() })

		event := testutil.EventFactory.Any()
		req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
		require.NoError(t, err)
		return req
	}

	t.Run("marks the request replayable without a header value", func(t *testing.T) {
		t.Parallel()
		req := format(t, "")
		values, ok := req.Header["Idempotency-Key"]
		assert.True(t, ok)
		assert.Nil(t, values)
		assert.NotNil(t, req.GetBody, "body must be rewindable for net/http to resend it")
	})

	t.Run("keeps a configured Idempotency-Key", func(t *testing.T) {
		t.Parallel()
		req := format(t, `{"Idempotency-Key":"from-config"}`)
		assert.Equal(t, []string{"from-config"}, req.Header["Idempotency-Key"])
	})

	t.Run("does not add Idempotency-Key next to a configured X-Idempotency-Key", func(t *testing.T) {
		t.Parallel()
		req := format(t, `{"x-idempotency-key":"from-config"}`)
		assert.Equal(t, "from-config", req.Header.Get("X-Idempotency-Key"))
		_, ok := req.Header["Idempotency-Key"]
		assert.False(t, ok)
	})
}
