package destwebhook_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/stretchr/testify/assert"
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
