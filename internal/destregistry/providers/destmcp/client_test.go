package destmcp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPClient(t *testing.T) {
	t.Parallel()

	t.Run("returns redirects instead of following them", func(t *testing.T) {
		t.Parallel()
		target := newRecorder(t, nil)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL+"/elsewhere", http.StatusFound)
		}))
		t.Cleanup(srv.Close)

		client := newClient(t, loopbackGuard(t), destregistry.HTTPClientConfig{})
		resp, err := client.Get(srv.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusFound, resp.StatusCode)
		assert.Empty(t, target.Requests())
	})

	t.Run("caps response headers", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Big", strings.Repeat("a", 65<<10))
		}))
		t.Cleanup(srv.Close)

		client := newClient(t, loopbackGuard(t), destregistry.HTTPClientConfig{})
		resp, err := client.Get(srv.URL)
		if err == nil {
			resp.Body.Close()
		}
		require.Error(t, err)
	})

	t.Run("does not ask for gzip", func(t *testing.T) {
		t.Parallel()
		got := make(chan string, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got <- r.Header.Get("Accept-Encoding")
		}))
		t.Cleanup(srv.Close)

		client := newClient(t, loopbackGuard(t), destregistry.HTTPClientConfig{})
		resp, err := client.Get(srv.URL)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Empty(t, <-got)
	})

	t.Run("requires a guard", func(t *testing.T) {
		t.Parallel()
		_, err := destmcp.NewHTTPClient(nil, destregistry.HTTPClientConfig{})
		require.Error(t, err)
	})
}

func TestRetryPolicy(t *testing.T) {
	t.Parallel()

	t.Run("configured", func(t *testing.T) {
		t.Parallel()
		b := &backoff.ConstantBackoff{Interval: time.Second}
		p := newProvider(t, func(c *destmcp.Config) {
			c.RetryBackoff = b
			c.RetryMaxRetries = 3
		})
		got, max, ok := p.RetryPolicy()
		assert.True(t, ok)
		assert.Equal(t, b, got)
		assert.Equal(t, 3, max)
	})

	t.Run("unset falls back to the global policy", func(t *testing.T) {
		t.Parallel()
		_, _, ok := newProvider(t).RetryPolicy()
		assert.False(t, ok)
	})
}
