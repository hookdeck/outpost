package destwebhook_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/proxychain/proxychaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deadURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	return u
}

func assertExternalProxyError(t *testing.T, err error, destHost string, hop int) *destregistry.ErrProxyDestination {
	t.Helper()
	require.Error(t, err)
	var destErr *destregistry.ErrProxyDestination
	require.True(t, errors.As(err, &destErr), "got %v", err)
	assert.Equal(t, destregistry.ProxyErrorCode, destErr.Code)
	assert.Equal(t, destHost, destErr.DestHost)
	assert.Equal(t, strconv.Itoa(hop), destErr.Diagnostics["proxy_hop"])
	assert.False(t, destregistry.IsProxyInfraError(err), "must not nack: %v", err)
	return destErr
}

func TestProxyTransport_ExternalHops_SingleHop(t *testing.T) {
	t.Parallel()

	t.Run("unreachable is a failed attempt, bracketless host", func(t *testing.T) {
		t.Parallel()
		client := makeProxiedClientExternal(t, deadURL(t), 1)
		_, err := client.Get("http://[::1]:9/")
		assertExternalProxyError(t, err, "::1", 0)
		_, err = client.Get("https://example.invalid/")
		assertExternalProxyError(t, err, "example.invalid", 0)
	})

	t.Run("CONNECT 407 is a failed attempt", func(t *testing.T) {
		t.Parallel()
		proxy := newCONNECTRejectingProxy(t, http.StatusProxyAuthRequired)
		client := makeProxiedClientExternal(t, proxy.URL, 1)
		_, err := client.Get("https://example.invalid/")
		destErr := assertExternalProxyError(t, err, "example.invalid", 0)
		proxyURL, _ := url.Parse(proxy.URL)
		assert.Equal(t, "http://"+proxyURL.Host, destErr.Diagnostics["proxy"])
	})

	t.Run("5xx stays the destination's code", func(t *testing.T) {
		t.Parallel()
		proxy := newCONNECTRejectingProxy(t, http.StatusServiceUnavailable)
		client := makeProxiedClientExternal(t, proxy.URL, 1)
		_, err := client.Get("https://example.invalid/")
		var destErr *destregistry.ErrProxyDestination
		require.True(t, errors.As(err, &destErr), "got %v", err)
		assert.Equal(t, "connection_refused", destErr.Code)
	})

	t.Run("failed attempt text carries no proxy address", func(t *testing.T) {
		t.Parallel()
		proxy := newCONNECTRejectingProxy(t, http.StatusProxyAuthRequired)
		proxyURL, _ := url.Parse(proxy.URL)
		client := makeProxiedClientExternal(t, proxy.URL, 1)
		req, err := http.NewRequest(http.MethodGet, "https://example.invalid/", nil)
		require.NoError(t, err)
		result := destwebhook.ExecuteHTTPRequest(context.Background(), client, req, "webhook", 0)
		require.NotNil(t, result.Delivery)
		assert.Equal(t, "failed", result.Delivery.Status)
		assert.Equal(t, destregistry.ProxyErrorCode, result.Delivery.Code)
		assert.Nil(t, result.Delivery.Response)
		assert.NotContains(t, result.Delivery.Code, proxyURL.Host)
		var attemptErr *destregistry.ErrDestinationPublishAttempt
		require.True(t, errors.As(result.Error, &attemptErr))
		assert.Equal(t, "0", attemptErr.Data["proxy_hop"])
		assert.Equal(t, "http://"+proxyURL.Host, attemptErr.Data["proxy"])
	})
}

// A chain [operator hop, external hop] with one external hop: failures at
// the operator hop nack, failures at or reaching the external hop are
// attempts.
func TestProxyTransport_ExternalHops_MixedChain(t *testing.T) {
	t.Parallel()
	flagged := map[string]string{"x-envoy-response-flags": "UF"}

	t.Run("operator hop unreachable nacks", func(t *testing.T) {
		t.Parallel()
		client := makeProxiedClientExternal(t, deadURL(t)+" http://"+nextHopHost, 1)
		_, err := client.Get("https://example.invalid/")
		var infraErr *destregistry.ErrProxyInfra
		require.True(t, errors.As(err, &infraErr), "got %v", err)
		assert.Equal(t, 0, infraErr.HopIndex)
	})

	t.Run("hang-up at a hop during CONNECT follows that hop", func(t *testing.T) {
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
		chain := "http://" + ln.Addr().String() + " http://" + nextHopHost

		_, err = makeProxiedClientExternal(t, chain, 1).Get("https://example.invalid/")
		var infraErr *destregistry.ErrProxyInfra
		require.True(t, errors.As(err, &infraErr), "got %v", err)
		assert.Equal(t, 0, infraErr.HopIndex)

		_, err = makeProxiedClientExternal(t, chain, 2).Get("https://example.invalid/")
		assertExternalProxyError(t, err, "example.invalid", 0)
	})

	t.Run("operator hop 407 nacks", func(t *testing.T) {
		t.Parallel()
		hop0 := newCONNECTRejectingProxy(t, http.StatusProxyAuthRequired)
		client := makeProxiedClientExternal(t, hop0.URL+" http://"+nextHopHost, 1)
		_, err := client.Get("https://example.invalid/")
		var infraErr *destregistry.ErrProxyInfra
		require.True(t, errors.As(err, &infraErr), "got %v", err)
		assert.Equal(t, 0, infraErr.HopIndex)
		assert.Equal(t, "example.invalid", infraErr.DestHost)
	})

	t.Run("operator hop cannot reach the external hop", func(t *testing.T) {
		t.Parallel()
		hop0 := newCONNECTRejectingProxyWithHeaders(t, http.StatusServiceUnavailable, flagged)
		chain := hop0.URL + " http://" + nextHopHost

		_, err := makeProxiedClientExternal(t, chain, 1).Get("https://example.invalid/")
		destErr := assertExternalProxyError(t, err, "example.invalid", 1)
		assert.Equal(t, "http://"+nextHopHost, destErr.Diagnostics["proxy"])
		assert.Equal(t, "UF", destErr.Diagnostics["envoy_flag"])

		_, err = makeProxiedClientExternal(t, chain, 0).Get("https://example.invalid/")
		var plain *destregistry.ErrProxyDestination
		require.True(t, errors.As(err, &plain), "got %v", err)
		assert.Equal(t, "connection_refused", plain.Code)
		assert.NotContains(t, plain.Diagnostics, "proxy_hop")
	})

	t.Run("external hop 407 on CONNECT to the target", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		hop1 := proxychaintest.New(t, false)
		hop1.Reject = func(string) (int, http.Header) { return http.StatusProxyAuthRequired, nil }
		chain := hop0.URL + " " + hop1.URL

		_, err := makeProxiedClientExternal(t, chain, 1).Get("https://example.invalid/")
		assertExternalProxyError(t, err, "example.invalid", 1)

		_, err = makeProxiedClientExternal(t, chain, 0).Get("https://example.invalid/")
		var infraErr *destregistry.ErrProxyInfra
		require.True(t, errors.As(err, &infraErr), "got %v", err)
		assert.Equal(t, 1, infraErr.HopIndex)
	})

	t.Run("external hop unreachable from the operator hop", func(t *testing.T) {
		t.Parallel()
		hop0 := proxychaintest.New(t, false)
		dead := deadURL(t)
		_, err := makeProxiedClientExternal(t, hop0.URL+" "+dead, 1).Get("https://example.invalid/")
		assertExternalProxyError(t, err, "example.invalid", 1)
	})
}

func TestProxyTransport_ExternalHops_CancelStaysInfra(t *testing.T) {
	t.Parallel()
	client := makeProxiedClientExternal(t, deadURL(t), 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid/", nil)
	require.NoError(t, err)
	_, err = client.Do(req)
	require.Error(t, err)
	var destErr *destregistry.ErrProxyDestination
	assert.False(t, errors.As(err, &destErr), "got %v", err)
}
