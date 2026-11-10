package destregistry_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
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
	return proxyDialFuncExternal(t, chain, 0)
}

func proxyDialFuncExternal(t *testing.T, chain string, externalHops int) proxychain.DialFunc {
	t.Helper()
	hops, err := proxychain.Parse(chain)
	require.NoError(t, err)
	return destregistry.ProxyDialFunc(hops, externalHops)
}

func TestProxyDialFunc_NoHops(t *testing.T) {
	t.Parallel()
	assert.Nil(t, destregistry.ProxyDialFunc(nil, 0))
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

func TestProxyDialFunc_ExternalHops(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const target = "broker.example.com:5672"
	flagged := http.Header{"X-Envoy-Response-Flags": {"UF"}}

	assertProxyError := func(t *testing.T, err error, hop int, hopURL string) {
		t.Helper()
		var destErr *destregistry.ErrProxyDestination
		require.True(t, errors.As(err, &destErr), "got %v", err)
		assert.Equal(t, destregistry.ProxyErrorCode, destErr.Code)
		assert.Equal(t, strconv.Itoa(hop), destErr.Diagnostics["proxy_hop"])
		assert.Equal(t, hopURL, destErr.Diagnostics["proxy"])
		delivery, perr := destregistry.ProxyPublishResult(err, "rabbitmq")
		require.NotNil(t, delivery)
		assert.Equal(t, "failed", delivery.Status)
		assert.Equal(t, destregistry.ProxyErrorCode, delivery.Code)
		assert.Equal(t, "proxy_error connecting to broker.example.com", delivery.Response["error"])
		assert.NotContains(t, delivery.Response["error"], strings.TrimPrefix(strings.TrimPrefix(hopURL, "https://"), "http://"))
		require.Error(t, perr)
	}
	assertInfra := func(t *testing.T, err error, hop int) {
		t.Helper()
		var infraErr *destregistry.ErrProxyInfra
		require.True(t, errors.As(err, &infraErr), "got %v", err)
		assert.Equal(t, hop, infraErr.HopIndex)
	}

	t.Run("single hop unreachable is a failed attempt", func(t *testing.T) {
		t.Parallel()
		_, err := proxyDialFuncExternal(t, "http://127.0.0.1:1", 1)(ctx, "tcp", target)
		assertProxyError(t, err, 0, "http://127.0.0.1:1")
	})

	t.Run("single hop auth rejected is a failed attempt", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Reject = func(string) (int, http.Header) { return http.StatusProxyAuthRequired, nil }
		_, err := proxyDialFuncExternal(t, hop.URL, 1)(ctx, "tcp", target)
		assertProxyError(t, err, 0, "http://"+proxychaintest.HostOf(hop.URL))
	})

	t.Run("upstream failure at the last hop stays the Envoy code", func(t *testing.T) {
		t.Parallel()
		hop := proxychaintest.New(t, false)
		hop.Reject = func(string) (int, http.Header) { return http.StatusServiceUnavailable, flagged }
		_, err := proxyDialFuncExternal(t, hop.URL, 1)(ctx, "tcp", target)
		var destErr *destregistry.ErrProxyDestination
		require.True(t, errors.As(err, &destErr), "got %v", err)
		assert.Equal(t, "connection_refused", destErr.Code)
	})

	t.Run("mixed chain, operator hop unreachable nacks", func(t *testing.T) {
		t.Parallel()
		hop1 := proxychaintest.New(t, false)
		_, err := proxyDialFuncExternal(t, "http://127.0.0.1:1 "+hop1.URL, 1)(ctx, "tcp", target)
		assertInfra(t, err, 0)
	})

	t.Run("mixed chain, operator hop auth rejected nacks", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop0.Reject = func(string) (int, http.Header) { return http.StatusProxyAuthRequired, nil }
		hop1 := proxychaintest.New(t, false)
		_, err := proxyDialFuncExternal(t, hop0.URL+" "+hop1.URL, 1)(ctx, "tcp", target)
		assertInfra(t, err, 0)
	})

	t.Run("mixed chain, operator hop cannot reach the external hop", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop0.Reject = func(string) (int, http.Header) { return http.StatusServiceUnavailable, flagged }
		hop1 := proxychaintest.New(t, false)
		chain := hop0.URL + " " + hop1.URL

		_, err := proxyDialFuncExternal(t, chain, 1)(ctx, "tcp", target)
		assertProxyError(t, err, 1, "http://"+proxychaintest.HostOf(hop1.URL))

		_, err = proxyDialFuncExternal(t, chain, 0)(ctx, "tcp", target)
		var destErr *destregistry.ErrProxyDestination
		require.True(t, errors.As(err, &destErr), "got %v", err)
		assert.Equal(t, "connection_refused", destErr.Code)
		assert.NotContains(t, destErr.Diagnostics, "proxy_hop")
		assert.Equal(t, "broker.example.com", destErr.DestHost)
		delivery, _ := destregistry.ProxyPublishResult(err, "rabbitmq")
		assert.Equal(t, "connection_refused connecting to broker.example.com", delivery.Response["error"])
	})

	t.Run("hang-up at an external intermediate hop", func(t *testing.T) {
		t.Parallel()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { ln.Close() })
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
		hop := "http://" + ln.Addr().String()
		_, err = proxyDialFuncExternal(t, hop+" http://127.0.0.1:2 http://127.0.0.1:3", 3)(ctx, "tcp", target)
		assertProxyError(t, err, 0, hop)
		_, err = proxyDialFuncExternal(t, hop+" http://127.0.0.1:2 http://127.0.0.1:3", 2)(ctx, "tcp", target)
		assertInfra(t, err, 0)
	})

	t.Run("mixed chain, external hop auth rejected is a failed attempt", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop1 := proxychaintest.New(t, false)
		hop1.Reject = func(string) (int, http.Header) { return http.StatusProxyAuthRequired, nil }
		chain := hop0.URL + " " + hop1.URL

		_, err := proxyDialFuncExternal(t, chain, 1)(ctx, "tcp", target)
		assertProxyError(t, err, 1, "http://"+proxychaintest.HostOf(hop1.URL))

		_, err = proxyDialFuncExternal(t, chain, 0)(ctx, "tcp", target)
		assertInfra(t, err, 1)
	})

	t.Run("mixed chain, external hop upstream failure is the destination", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop1 := proxychaintest.New(t, false)
		hop1.Reject = func(string) (int, http.Header) { return http.StatusServiceUnavailable, flagged }
		_, err := proxyDialFuncExternal(t, hop0.URL+" "+hop1.URL, 1)(ctx, "tcp", target)
		var destErr *destregistry.ErrProxyDestination
		require.True(t, errors.As(err, &destErr), "got %v", err)
		assert.Equal(t, "connection_refused", destErr.Code)
	})

	t.Run("TLS failure at the external hop is a failed attempt", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop1 := proxychaintest.New(t, true)
		_, err := proxyDialFuncExternal(t, hop0.URL+" "+hop1.URL, 1)(ctx, "tcp", target)
		assertProxyError(t, err, 1, "https://"+proxychaintest.HostOf(hop1.URL))
	})

	t.Run("cancel while dialing an external hop stays infra", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := proxyDialFuncExternal(t, "http://127.0.0.1:1", 1)(ctx, "tcp", target)
		assertInfra(t, err, 0)
	})

	t.Run("first hop dial timeout stays infra without external hops", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithDeadline(ctx, time.Now())
		defer cancel()
		_, err := proxyDialFunc(t, "http://127.0.0.1:1")(ctx, "tcp", target)
		assertInfra(t, err, 0)
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

	t.Run("proxy infra data locates the hop without credentials", func(t *testing.T) {
		t.Parallel()
		hops, err := proxychain.Parse("http://u:topsecret@127.0.0.1:1")
		require.NoError(t, err)
		_, derr := destregistry.ProxyDialFunc(hops, 0)(context.Background(), "tcp", "broker:5672")
		_, perr := destregistry.ProxyPublishResult(derr, "rabbitmq")
		var attemptErr *destregistry.ErrDestinationPublishAttempt
		require.True(t, errors.As(perr, &attemptErr))
		assert.Equal(t, "0", attemptErr.Data["proxy_hop"])
		assert.Equal(t, "http://127.0.0.1:1", attemptErr.Data["proxy"])
		assert.Contains(t, attemptErr.Data["proxy_error"], "connection refused")
		assert.NotContains(t, fmt.Sprint(attemptErr.Data), "topsecret")
		assert.NotContains(t, attemptErr.Data["message"], "127.0.0.1")
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
