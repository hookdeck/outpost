package mcpevents

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    *Error
		reason string
		data   map[string]any
	}{
		{"invalid_params", InvalidParams(FieldURL, ReasonHTTPSRequired), ReasonHTTPSRequired,
			map[string]any{"field": "config.url", "reason": "https_required"}},
		{"resource_exhausted", ResourceExhausted(LimitVerificationRate, 10, 1500*time.Millisecond), LimitVerificationRate,
			map[string]any{"limit": "verification_rate", "max": 10, "retryAfterMs": int64(1500)}},
		{"resource_exhausted bare", ResourceExhausted(LimitVerificationRate, 0, 0), LimitVerificationRate,
			map[string]any{"limit": "verification_rate"}},
		{"callback_endpoint_error", CallbackEndpointError(ReasonTimeout), ReasonTimeout,
			map[string]any{"reason": "timeout"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.reason, tt.err.Reason())
			assert.Equal(t, tt.data, tt.err.Data)
		})
	}
}

func TestErrorKindsHaveSEPCodes(t *testing.T) {
	t.Parallel()
	for k, code := range map[Kind]int{
		KindInvalidParams:         -32602,
		KindNotFound:              -32023,
		KindForbidden:             -32024,
		KindResourceExhausted:     -32025,
		KindUnsupported:           -32026,
		KindCallbackEndpointError: -32027,
	} {
		assert.Equal(t, code, (&Error{Kind: k}).Code(), k)
		assert.NotEmpty(t, kinds[k].message, k)
	}
	assert.Equal(t, -32603, (&Error{Kind: "other"}).Code())
	assert.Equal(t, -32023, NotFound(NotFoundSubscription).Code())
	assert.Equal(t, map[string]any{"kind": "subscription"}, NotFound(NotFoundSubscription).Data)
	assert.Equal(t, "NotFound", NotFound(NotFoundEvent).Message)
}
