package proxychain_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
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
	assert.NotContains(t, err.Error(), "topsecret")
}
