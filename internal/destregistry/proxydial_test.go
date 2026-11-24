package destregistry_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/hookdeck/outpost/internal/proxychain/proxychaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func proxyDialFunc(t *testing.T, chain string) proxychain.DialFunc {
	t.Helper()
	hops, err := proxychain.Parse(chain)
	require.NoError(t, err)
	return destregistry.ProxyDialFunc(hops)
}

func TestProxyDialFunc_NoHops(t *testing.T) {
	t.Parallel()
	assert.Nil(t, destregistry.ProxyDialFunc(nil))
}

func TestProxyDialFunc_TunnelsThroughChain(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 5)
		if _, err := io.ReadFull(conn, buf); err == nil {
			_, _ = conn.Write(buf)
		}
	}()

	hop0 := proxychaintest.New(t, false)
	hop1 := proxychaintest.New(t, false)
	dial := proxyDialFunc(t, hop0.URL+" "+hop1.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dial(ctx, "tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	_, err = io.WriteString(conn, "hello")
	require.NoError(t, err)
	b, err := io.ReadAll(conn)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(b))
	assert.Equal(t, []string{proxychaintest.HostOf(hop1.URL)}, hop0.Connects())
	assert.Equal(t, []string{ln.Addr().String()}, hop1.Connects())
}

func TestProxyDialFunc_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("first hop unreachable is proxy infra", func(t *testing.T) {
		t.Parallel()
		_, err := proxyDialFunc(t, "http://127.0.0.1:1")(ctx, "tcp", "broker.example.com:5672")
		var infraErr *destregistry.ErrProxyInfra
		require.True(t, errors.As(err, &infraErr), "got %v", err)
		assert.Equal(t, "broker.example.com", infraErr.DestHost)
	})

	t.Run("proxy auth rejected is proxy infra", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Reject = func(string) (int, http.Header) { return http.StatusProxyAuthRequired, nil }
		_, err := proxyDialFunc(t, hop.URL)(ctx, "tcp", "broker.example.com:5672")
		var infraErr *destregistry.ErrProxyInfra
		require.True(t, errors.As(err, &infraErr), "got %v", err)
	})

	t.Run("RBAC deny is network_unreachable", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Reject = func(string) (int, http.Header) {
			return http.StatusForbidden, http.Header{"X-Envoy-Response-Code-Details": {"rbac_access_denied_matched_policy[none]"}}
		}
		_, err := proxyDialFunc(t, hop.URL)(ctx, "tcp", "broker.example.com:5672")
		var destErr *destregistry.ErrProxyDestination
		require.True(t, errors.As(err, &destErr), "got %v", err)
		assert.Equal(t, "network_unreachable", destErr.Code)
		assert.Equal(t, "broker.example.com", destErr.DestHost)
	})

	t.Run("upstream failure maps the Envoy flag", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Reject = func(string) (int, http.Header) {
			return http.StatusServiceUnavailable, http.Header{"X-Envoy-Response-Flags": {"UF"}}
		}
		_, err := proxyDialFunc(t, hop.URL)(ctx, "tcp", "broker.example.com:5672")
		var destErr *destregistry.ErrProxyDestination
		require.True(t, errors.As(err, &destErr), "got %v", err)
		assert.Equal(t, "connection_refused", destErr.Code)
	})

	t.Run("unanswered CONNECT times out", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Hold = make(chan struct{})
		t.Cleanup(func() { close(hop.Hold) })
		ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		_, err := proxyDialFunc(t, hop.URL)(ctx, "tcp", "broker.example.com:5672")
		require.Error(t, err)
		assert.False(t, destregistry.IsProxyError(err), "timeouts are left to the caller: %v", err)
		assert.True(t, strings.Contains(err.Error(), "timeout") || errors.Is(err, context.DeadlineExceeded), "got %v", err)
	})

	t.Run("unanswered CONNECT to the next hop times out", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop0.Hold = make(chan struct{})
		t.Cleanup(func() { close(hop0.Hold) })
		hop1 := proxychaintest.New(t, false)
		ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		_, err := proxyDialFunc(t, hop0.URL+" "+hop1.URL)(ctx, "tcp", "broker.example.com:5672")
		require.Error(t, err)
		assert.False(t, destregistry.IsProxyError(err), "timeouts are left to the caller: %v", err)
	})

	t.Run("TLS handshake timeout to a hop is proxy infra", func(t *testing.T) {
		t.Parallel()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		var mu sync.Mutex
		var conns []net.Conn
		t.Cleanup(func() {
			ln.Close()
			mu.Lock()
			defer mu.Unlock()
			for _, c := range conns {
				c.Close()
			}
		})
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				mu.Lock()
				conns = append(conns, conn)
				mu.Unlock()
			}
		}()
		ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		_, err = proxyDialFunc(t, "https://"+ln.Addr().String())(ctx, "tcp", "broker.example.com:5672")
		var infraErr *destregistry.ErrProxyInfra
		require.True(t, errors.As(err, &infraErr), "got %v", err)
	})

	t.Run("cancel passes through", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Hold = make(chan struct{})
		t.Cleanup(func() { close(hop.Hold) })
		ctx, cancel := context.WithCancel(ctx)
		time.AfterFunc(100*time.Millisecond, cancel)
		_, err := proxyDialFunc(t, hop.URL)(ctx, "tcp", "broker.example.com:5672")
		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, destregistry.IsProxyError(err))
	})
}

func TestProxyPublishResult(t *testing.T) {
	t.Parallel()

	t.Run("proxy infra nacks", func(t *testing.T) {
		t.Parallel()
		err := &destregistry.ErrProxyInfra{Underlying: errors.New("dial tcp: connection refused"), DestHost: "broker"}
		require.True(t, destregistry.IsProxyError(err))
		delivery, perr := destregistry.ProxyPublishResult(err, "rabbitmq")
		assert.Nil(t, delivery)
		var attemptErr *destregistry.ErrDestinationPublishAttempt
		require.True(t, errors.As(perr, &attemptErr))
		assert.Equal(t, "proxy_infrastructure", attemptErr.Data["error"])
	})

	t.Run("proxy destination is a failed attempt without diagnostics", func(t *testing.T) {
		t.Parallel()
		err := &destregistry.ErrProxyDestination{
			Underlying:  errors.New("proxy returned 403"),
			Code:        "network_unreachable",
			DestHost:    "broker",
			Diagnostics: map[string]string{"envoy_details": "rbac_access_denied"},
		}
		require.True(t, destregistry.IsProxyError(err))
		delivery, perr := destregistry.ProxyPublishResult(err, "kafka")
		require.NotNil(t, delivery)
		assert.Equal(t, "failed", delivery.Status)
		assert.Equal(t, "network_unreachable", delivery.Code)
		assert.Equal(t, map[string]interface{}{"error": "network_unreachable connecting to broker"}, delivery.Response)
		var attemptErr *destregistry.ErrDestinationPublishAttempt
		require.True(t, errors.As(perr, &attemptErr))
		assert.Equal(t, "rbac_access_denied", attemptErr.Data["envoy_details"])
	})

	t.Run("other errors are not proxy errors", func(t *testing.T) {
		t.Parallel()
		assert.False(t, destregistry.IsProxyError(errors.New("connection refused")))
	})
}
