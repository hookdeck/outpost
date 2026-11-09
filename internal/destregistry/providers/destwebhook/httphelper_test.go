package destwebhook_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHTTPResponse_MaxBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		maxBytes int
		wantBody string
	}{
		{
			name:     "no limit stores verbatim",
			body:     strings.Repeat("x", 5000),
			maxBytes: 0,
			wantBody: strings.Repeat("x", 5000),
		},
		{
			name:     "under limit stores verbatim",
			body:     "hello world",
			maxBytes: 1024,
			wantBody: "hello world",
		},
		{
			name:     "exactly at limit stores verbatim",
			body:     strings.Repeat("x", 1024),
			maxBytes: 1024,
			wantBody: strings.Repeat("x", 1024),
		},
		{
			name:     "over limit replaced with placeholder",
			body:     strings.Repeat("x", 2048),
			maxBytes: 1024,
			wantBody: "Response body exceeded 1024 bytes and was not stored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}
			delivery := &destregistry.Delivery{}

			destwebhook.ParseHTTPResponse(delivery, resp, tt.maxBytes)

			assert.Equal(t, http.StatusOK, delivery.Response["status"], "status should be preserved")
			assert.Equal(t, tt.wantBody, delivery.Response["body"])
		})
	}
}

func TestExecuteHTTPRequest_RedirectIsFailure(t *testing.T) {
	t.Parallel()

	for _, code := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/elsewhere")
				w.WriteHeader(code)
			}))
			defer server.Close()

			req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{}"))
			require.NoError(t, err)

			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			}}
			result := destwebhook.ExecuteHTTPRequest(context.Background(), client, req, "webhook", 0)

			require.NotNil(t, result.Delivery)
			assert.Equal(t, "failed", result.Delivery.Status)
			assert.Equal(t, fmt.Sprintf("%d", code), result.Delivery.Code)
			assert.Equal(t, code, result.Delivery.Response["status"])
			assert.Error(t, result.Error)
		})
	}
}

func TestParseHTTPResponse_Location(t *testing.T) {
	t.Parallel()

	long := "https://example.com/" + strings.Repeat("a", 5000)
	tests := []struct {
		name     string
		status   int
		location string
		want     any
	}{
		{"3xx with location", 302, "https://example.com/new", "https://example.com/new"},
		{"3xx without location", 300, "", nil},
		{"3xx location is capped", 301, long, long[:2048]},
		{"2xx ignores location", 200, "https://example.com/new", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			if tt.location != "" {
				header.Set("Location", tt.location)
			}
			resp := &http.Response{StatusCode: tt.status, Header: header, Body: io.NopCloser(strings.NewReader(""))}
			delivery := &destregistry.Delivery{}
			destwebhook.ParseHTTPResponse(delivery, resp, 0)

			if tt.want == nil {
				assert.NotContains(t, delivery.Response, "location")
				return
			}
			assert.Equal(t, tt.want, delivery.Response["location"])
		})
	}
}

func TestExecuteHTTPRequest_ServerClosesBeforeResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		conn, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		conn.Close()
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{}"))
	require.NoError(t, err)

	result := destwebhook.ExecuteHTTPRequest(context.Background(), server.Client(), req, "webhook", 0)

	require.NotNil(t, result.Delivery)
	assert.Equal(t, "failed", result.Delivery.Status)
	assert.Equal(t, "connection_reset", result.Delivery.Code)
	assert.Error(t, result.Error)
}
