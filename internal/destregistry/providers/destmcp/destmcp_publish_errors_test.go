package destmcp_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublish_Statuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status       int
		success      bool
		nonRetryable bool
	}{
		{200, true, false},
		{201, true, false},
		{202, true, false},
		{204, true, false},
		{299, true, false},
		{301, false, false},
		{302, false, false},
		{304, false, false},
		{307, false, false},
		{400, false, false},
		{401, false, false},
		{404, false, false},
		{410, false, true},
		{413, false, true},
		{429, false, false},
		{500, false, false},
		{503, false, false},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			t.Parallel()
			redirected := newRecorder(t, nil)
			rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", redirected.URL+"/elsewhere")
				w.WriteHeader(tt.status)
				if tt.status != http.StatusNoContent && tt.status != http.StatusNotModified {
					_, _ = io.WriteString(w, "body-"+strconv.Itoa(tt.status))
				}
			})
			p := localProvider(t)
			delivery, err := publish(t, p, localSubscription(t, p, rec.URL+"/hook"), testEvent())

			require.NotNil(t, delivery)
			assert.Equal(t, strconv.Itoa(tt.status), delivery.Code)
			assert.Equal(t, tt.status, delivery.Response["status"])
			if tt.success {
				assert.NoError(t, err)
				assert.Equal(t, models.AttemptStatusSuccess, delivery.Status)
			} else {
				assert.Equal(t, models.AttemptStatusFailed, delivery.Status)
				pubErr := requirePublishErr(t, err)
				assert.Equal(t, tt.nonRetryable, pubErr.NonRetryable)
				assert.Equal(t, tt.nonRetryable, pubErr.SkipFailureCount)
				assert.NotContains(t, pubErr.Data, "body", "response content stays out of logs")
			}
			assert.Len(t, rec.Requests(), 1)
			assert.Empty(t, redirected.Requests(), "redirects are never followed")
		})
	}
}

func TestPublish_ResponseBody(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 10<<10)
	tests := []struct {
		name     string
		max      int
		body     string
		want     string
		wantNone bool
	}{
		{name: "small body", body: "ok", want: "ok"},
		{name: "default cap", body: big, want: big[:destmcp.DefaultMaxResponseBodyBytes]},
		{name: "custom cap", max: 16, body: big, want: big[:16]},
		{name: "not stored", max: -1, body: big, wantNone: true},
		{name: "empty body", body: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, tt.body)
			})
			p := localProvider(t, func(c *destmcp.Config) { c.MaxResponseBodyBytes = tt.max })
			delivery, err := publish(t, p, localSubscription(t, p, rec.URL+"/hook"), testEvent())
			require.Error(t, err)
			require.NotNil(t, delivery)
			if tt.wantNone {
				assert.NotContains(t, delivery.Response, "body")
			} else {
				assert.Equal(t, tt.want, delivery.Response["body"])
			}
			assert.Equal(t, http.StatusBadGateway, delivery.Response["status"])
		})
	}
}

func TestPublish_PayloadTooLarge(t *testing.T) {
	t.Parallel()
	rec := newRecorder(t, nil)
	p := localProvider(t)
	event := testEvent()
	event.Data = json.RawMessage(`{"blob":"` + strings.Repeat("x", mcpevents.MaxEnvelopeBytes) + `"}`)

	delivery, err := publish(t, p, localSubscription(t, p, rec.URL+"/hook"), event)
	require.NotNil(t, delivery, "a failed attempt is recorded")
	assert.Equal(t, models.AttemptStatusFailed, delivery.Status)
	assert.Equal(t, destmcp.CodePayloadTooLarge, delivery.Code)
	pubErr := requirePublishErr(t, err)
	assert.True(t, pubErr.NonRetryable)
	assert.True(t, pubErr.SkipFailureCount)
	assert.Empty(t, rec.Requests(), "nothing is sent")

	// Just under the limit is sent.
	envelopeOverhead := len(`{"eventId":"","name":"","timestamp":"2026-10-09T16:58:12Z","data":,"cursor":null}`) + len(event.ID) + len(event.Topic)
	event.Data = json.RawMessage(`"` + strings.Repeat("x", mcpevents.MaxEnvelopeBytes-envelopeOverhead-2) + `"`)
	delivery, err = publish(t, p, localSubscription(t, p, rec.URL+"/hook"), event)
	require.NoError(t, err)
	assert.Equal(t, "200", delivery.Code)
	reqs := rec.Requests()
	require.Len(t, reqs, 1)
	assert.Len(t, reqs[0].Body, mcpevents.MaxEnvelopeBytes)
}

func TestPublish_InvalidEventData(t *testing.T) {
	t.Parallel()
	rec := newRecorder(t, nil)
	p := localProvider(t)
	event := testEvent()
	event.Data = json.RawMessage(`{"nope"`)
	delivery, err := publish(t, p, localSubscription(t, p, rec.URL+"/hook"), event)
	require.NotNil(t, delivery)
	assert.Equal(t, "ERR", delivery.Code)
	pubErr := requirePublishErr(t, err)
	assert.True(t, pubErr.NonRetryable)
	assert.False(t, pubErr.SkipFailureCount)
	assert.Empty(t, rec.Requests())
}

func TestPublish_AddressNotAllowed(t *testing.T) {
	t.Parallel()

	t.Run("https to a blocked address", func(t *testing.T) {
		t.Parallel()
		var hits atomic.Int32
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
		t.Cleanup(srv.Close)
		roots := x509.NewCertPool()
		roots.AddCert(srv.Certificate())
		// No allowlist: 127.0.0.1 is not globally routable.
		guard := &netguard.Guard{}
		p := newProvider(t, func(c *destmcp.Config) {
			c.Guard = guard
			c.Client = tlsClient(guard, roots)
		})
		delivery, err := publish(t, p, localSubscription(t, p, srv.URL+"/hook"), testEvent())
		require.NotNil(t, delivery)
		assert.Equal(t, models.AttemptStatusFailed, delivery.Status)
		assert.Equal(t, "address_not_allowed", delivery.Code)
		pubErr := requirePublishErr(t, err)
		assert.False(t, pubErr.NonRetryable, "the address may become allowed again")
		assert.False(t, pubErr.SkipFailureCount)
		assert.Zero(t, hits.Load())
	})

	t.Run("plain http to a blocked address", func(t *testing.T) {
		t.Parallel()
		rec := newRecorder(t, nil)
		guard := &netguard.Guard{}
		p := newProvider(t, func(c *destmcp.Config) {
			c.Guard = guard
			c.Client = newClient(t, guard, destregistry.HTTPClientConfig{})
		})
		delivery, err := publish(t, p, localSubscription(t, p, rec.URL+"/hook"), testEvent())
		require.Error(t, err)
		assert.Equal(t, "address_not_allowed", delivery.Code)
		assert.Empty(t, rec.Requests())
	})

}

// Not parallel: the parallel tests' servers would race for the closed port.
func TestPublish_NetworkErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	p := localProvider(t)
	delivery, err := publish(t, p, localSubscription(t, p, "http://"+addr+"/hook"), testEvent())
	require.NotNil(t, delivery)
	assert.Equal(t, "connection_refused", delivery.Code)
	pubErr := requirePublishErr(t, err)
	assert.False(t, pubErr.NonRetryable)
	assert.False(t, pubErr.SkipFailureCount)
	assert.NotContains(t, pubErr.Data["message"], "/hook", "the URL stays out of the logged message")
}

func TestPublish_ContextCancelled(t *testing.T) {
	t.Parallel()
	rec := newRecorder(t, nil)
	p := localProvider(t)
	pub, err := p.CreatePublisher(context.Background(), localSubscription(t, p, rec.URL+"/hook"))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	delivery, err := pub.Publish(ctx, testEvent())
	assert.Nil(t, delivery, "shutdown requeues the message instead of recording an attempt")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPublish_ErrorsAreClassified(t *testing.T) {
	t.Parallel()
	// A deadline hit while waiting for the response is a timeout, not a
	// generic network error.
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
	p := localProvider(t)
	pub, err := p.CreatePublisher(context.Background(), localSubscription(t, p, rec.URL+"/hook"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	delivery, err := pub.Publish(ctx, testEvent())
	require.NotNil(t, delivery)
	assert.Equal(t, "timeout", delivery.Code)
	pubErr := requirePublishErr(t, err)
	assert.True(t, errors.Is(pubErr.Err, context.DeadlineExceeded))
}

func tlsClient(guard *netguard.Guard, roots *x509.CertPool) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:     guard.DialContext,
			TLSClientConfig: &tls.Config{RootCAs: roots},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
