package destregistry_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeHTTPClient_UserAgent(t *testing.T) {
	t.Parallel()

	provider, err := newMockProvider()
	require.NoError(t, err)

	t.Run("sets user agent on requests", func(t *testing.T) {
		t.Parallel()

		// Create a test server that captures the User-Agent header
		var capturedUserAgent string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedUserAgent = r.Header.Get("User-Agent")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create client with user agent
		userAgent := "TestAgent/1.0"
		client, err := provider.MakeHTTPClient(destregistry.HTTPClientConfig{
			UserAgent: &userAgent,
		})
		require.NoError(t, err)

		// Make a request
		resp, err := client.Get(server.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		io.ReadAll(resp.Body)

		// Verify user agent was set
		assert.Equal(t, "TestAgent/1.0", capturedUserAgent)
	})

	t.Run("handles empty user agent string", func(t *testing.T) {
		t.Parallel()

		emptyUserAgent := ""
		client, err := provider.MakeHTTPClient(destregistry.HTTPClientConfig{
			UserAgent: &emptyUserAgent,
		})
		require.NoError(t, err)

		// Should still create a valid client
		assert.NotNil(t, client)
		assert.NotNil(t, client.Transport)
	})
}

func TestMakeHTTPClient_Proxy(t *testing.T) {
	t.Parallel()

	provider, err := newMockProvider()
	require.NoError(t, err)

	t.Run("routes requests through proxy", func(t *testing.T) {
		t.Parallel()

		// Create a proxy server that tracks requests
		var proxyRequestReceived bool
		proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxyRequestReceived = true
			// For CONNECT requests (HTTPS), respond with 200
			if r.Method == http.MethodConnect {
				w.WriteHeader(http.StatusOK)
				return
			}
			// For regular HTTP requests, proxy them
			w.WriteHeader(http.StatusOK)
		}))
		defer proxyServer.Close()

		// Create a target server
		targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer targetServer.Close()

		// Create client with proxy configured
		client, err := provider.MakeHTTPClient(destregistry.HTTPClientConfig{
			ProxyURL: &proxyServer.URL,
		})
		require.NoError(t, err)

		// Make a request through the proxy
		resp, err := client.Get(targetServer.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		io.ReadAll(resp.Body)

		// Verify request went through proxy
		assert.True(t, proxyRequestReceived, "Expected request to go through proxy")
	})

	t.Run("returns error for invalid proxy URL", func(t *testing.T) {
		t.Parallel()

		invalidProxy := "://invalid-url"
		_, err := provider.MakeHTTPClient(destregistry.HTTPClientConfig{
			ProxyURL: &invalidProxy,
		})

		// Should return error for invalid proxy URL
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid proxy URL")
	})
}
