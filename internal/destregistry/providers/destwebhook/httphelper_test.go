package destwebhook_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
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

// Ephemeral port exhaustion is a source-side failure: the fix is connection
// reuse, not a retry against the destination. Folding it into the
// network_error catch-all alongside genuinely remote failures is what made a
// 7,604-failure episode in the 2026-07-30 benchmark unattributable.
func TestClassifyNetworkError_AddressUnavailable(t *testing.T) {
	t.Parallel()

	err := &url.Error{
		Op:  "Post",
		URL: "https://example.com/hook",
		Err: &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &os.SyscallError{Syscall: "connect", Err: syscall.EADDRNOTAVAIL},
		},
	}
	assert.Equal(t, "address_unavailable", destwebhook.ClassifyNetworkError(err))
}

func TestClassifyNetworkError_Codes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"dns", errors.New(`dial tcp: lookup nope.invalid: no such host`), "dns_error"},
		{"refused", errors.New(`dial tcp 1.2.3.4:443: connect: connection refused`), "connection_refused"},
		{"timeout", errors.New(`context deadline exceeded`), "timeout"},
		{"unknown", errors.New(`something else entirely`), "network_error"},
		{"nil", nil, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, destwebhook.ClassifyNetworkError(tt.err))
		})
	}
}

// A transport failure must record why it failed. This was the only one of the
// package's four failure paths leaving Delivery.Response nil, so every
// connection-level failure stored response_data NULL.
func TestExecuteHTTPRequest_TransportErrorRecordsCause(t *testing.T) {
	t.Parallel()

	// Port 1 on loopback: nothing listens, so client.Do fails at the transport
	// layer without involving a server.
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/hook", strings.NewReader("{}"))
	require.NoError(t, err)

	res := destwebhook.ExecuteHTTPRequest(context.Background(), http.DefaultClient, req, "webhook", 0)

	require.NotNil(t, res.Delivery, "transport error must still record a customer-visible attempt")
	assert.Equal(t, "failed", res.Delivery.Status)
	require.NotNil(t, res.Delivery.Response, "response_data was nil — the cause is unrecoverable after the fact")

	msg, ok := res.Delivery.Response["error"].(string)
	require.True(t, ok, "response_data.error missing or not a string: %#v", res.Delivery.Response)
	assert.Contains(t, msg, "connection refused")
	assert.Contains(t, msg, "127.0.0.1:1", "the destination the customer configured should be identifiable")
}
