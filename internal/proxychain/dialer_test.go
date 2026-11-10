package proxychain_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/hookdeck/outpost/internal/proxychain/proxychaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEchoServer accepts TCP connections and echoes whatever it reads.
func newEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

func dialThrough(t *testing.T, chain, addr string) (net.Conn, error) {
	t.Helper()
	hops, err := proxychain.Parse(chain)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return proxychain.NewDialer(hops, nil, nil).DialContext(ctx, "tcp", addr)
}

func assertEcho(t *testing.T, conn net.Conn) {
	t.Helper()
	_, err := conn.Write([]byte("ping"))
	require.NoError(t, err)
	buf := make([]byte, 4)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(buf))
}

func TestDialer_OneHop(t *testing.T) {
	t.Parallel()
	target := newEchoServer(t)
	hop := proxychaintest.New(t, false)

	conn, err := dialThrough(t, proxychaintest.WithCreds(hop.URL, "u", "pw"), target)
	require.NoError(t, err)
	defer conn.Close()
	assertEcho(t, conn)

	assert.Equal(t, []string{target}, hop.Connects())
	assert.Equal(t, []string{"Basic dTpwdw=="}, hop.Auths(), "u:pw")
}

func TestDialer_TwoHops(t *testing.T) {
	t.Parallel()
	target := newEchoServer(t)
	hop0 := proxychaintest.New(t, false)
	hop1 := proxychaintest.New(t, true)

	// hop1 is https: trust its test certificate for the TLS leg inside hop0's tunnel.
	hops, err := proxychain.Parse(hop0.URL + " " + hop1.URL)
	require.NoError(t, err)
	tlsConfig := hop1.Client().Transport.(*http.Transport).TLSClientConfig
	d := proxychain.NewDialer(hops, nil, func() *tls.Config { return tlsConfig })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", target)
	require.NoError(t, err)
	defer conn.Close()
	assertEcho(t, conn)

	assert.Equal(t, []string{proxychaintest.HostOf(hop1.URL)}, hop0.Connects())
	assert.Equal(t, []string{target}, hop1.Connects())
}

func TestDialer_AuthRejected(t *testing.T) {
	t.Parallel()
	target := newEchoServer(t)
	hop0 := proxychaintest.New(t, false)
	hop1 := proxychaintest.New(t, false)
	hop1.Reject = func(string) (int, http.Header) { return http.StatusProxyAuthRequired, nil }

	chain := hop0.URL + " " + proxychaintest.WithCreds(hop1.URL, "u", "topsecret")
	_, err := dialThrough(t, chain, target)
	require.Error(t, err)

	var connectErr *proxychain.ConnectError
	require.True(t, errors.As(err, &connectErr), "got %v", err)
	assert.Equal(t, http.StatusProxyAuthRequired, connectErr.Status)
	assert.Equal(t, "http://"+proxychaintest.HostOf(hop1.URL), connectErr.Hop)
	assert.Equal(t, target, connectErr.Next)
	assert.Equal(t, 1, connectErr.Index)
	assert.NotContains(t, err.Error(), "topsecret")
}

func TestDialer_HopErrorIndex(t *testing.T) {
	t.Parallel()
	target := newEchoServer(t)

	t.Run("first hop unreachable", func(t *testing.T) {
		t.Parallel()
		_, err := dialThrough(t, "http://127.0.0.1:1", target)
		var hopErr *proxychain.HopError
		require.True(t, errors.As(err, &hopErr), "got %v", err)
		assert.Equal(t, 0, hopErr.Index)
		assert.Equal(t, "http://127.0.0.1:1", hopErr.Hop)
	})

	t.Run("TLS to second hop fails", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop1 := proxychaintest.New(t, true)
		// Test certificate is not trusted.
		_, err := dialThrough(t, hop0.URL+" "+hop1.URL, target)
		var hopErr *proxychain.HopError
		require.True(t, errors.As(err, &hopErr), "got %v", err)
		assert.True(t, hopErr.Handshake)
		assert.Equal(t, 1, hopErr.Index)
	})

	t.Run("second hop answers CONNECT with 407", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop1 := proxychaintest.New(t, false)
		hop1.Reject = func(string) (int, http.Header) { return http.StatusProxyAuthRequired, nil }
		_, err := dialThrough(t, hop0.URL+" "+hop1.URL, target)
		var connectErr *proxychain.ConnectError
		require.True(t, errors.As(err, &connectErr), "got %v", err)
		assert.Equal(t, 1, connectErr.Index)
	})

	t.Run("first hop refuses CONNECT to second", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop0.Reject = func(string) (int, http.Header) { return http.StatusServiceUnavailable, nil }
		hop1 := proxychaintest.New(t, false)
		_, err := dialThrough(t, hop0.URL+" "+hop1.URL, target)
		var connectErr *proxychain.ConnectError
		require.True(t, errors.As(err, &connectErr), "got %v", err)
		assert.Equal(t, 0, connectErr.Index)
	})
}

func TestDialer_CancelWhileCONNECTUnanswered(t *testing.T) {
	t.Parallel()
	hop := proxychaintest.New(t, false)
	hop.Hold = make(chan struct{})
	t.Cleanup(func() { close(hop.Hold) })
	hops, err := proxychain.Parse(hop.URL)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err = proxychain.NewDialer(hops, nil, nil).DialContext(ctx, "tcp", "127.0.0.1:1")
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestDialer_HopErrorOnUnansweredCONNECT(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 2} {
		hops := make([]string, n)
		var held *proxychaintest.Proxy
		for i := range hops {
			p := proxychaintest.New(t, false)
			hops[i] = p.URL
			held = p
		}
		// Only the last hop leaves its CONNECT unanswered.
		held.Hold = make(chan struct{})
		t.Cleanup(func() { close(held.Hold) })
		chain, err := proxychain.Parse(strings.Join(hops, " "))
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, err = proxychain.NewDialer(chain, nil, nil).DialContext(ctx, "tcp", "127.0.0.1:1")
		cancel()
		var hopErr *proxychain.HopError
		require.True(t, errors.As(err, &hopErr), "got %v", err)
		assert.False(t, hopErr.Handshake)
		var netErr net.Error
		assert.True(t, errors.As(err, &netErr) && netErr.Timeout(), "got %v", err)
	}
}
