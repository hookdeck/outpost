package mcpevents

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestError_JSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    *Error
		sketch string
		sep    string
	}{
		{
			"invalid_params",
			InvalidParams(FieldDeliveryURL, ReasonAddressNotAllowed),
			`{"mcp_error":{"kind":"invalid_params","code":-32602,"message":"InvalidParams","data":{"field":"delivery.url","reason":"address_not_allowed"}}}`,
			`{"mcp_error":{"kind":"invalid_params","code":-32602,"message":"InvalidParams","data":{"field":"delivery.url","reason":"address_not_allowed"}}}`,
		},
		{
			"invalid_params with errors",
			InvalidParamsWithErrors(FieldArguments, ReasonSchemaMismatch, []string{"arguments.total: must be >= 0", "arguments: has properties that are not allowed <&>"}),
			`{"mcp_error":{"kind":"invalid_params","code":-32602,"message":"InvalidParams","data":{"errors":["arguments.total: must be >= 0","arguments: has properties that are not allowed <&>"],"field":"arguments","reason":"schema_mismatch"}}}`,
			`{"mcp_error":{"kind":"invalid_params","code":-32602,"message":"InvalidParams","data":{"errors":["arguments.total: must be >= 0","arguments: has properties that are not allowed <&>"],"field":"arguments","reason":"schema_mismatch"}}}`,
		},
		{
			"not_found",
			NotFound(NotFoundEvent),
			`{"mcp_error":{"kind":"not_found","code":-32011,"message":"NotFound","data":{"kind":"event"}}}`,
			`{"mcp_error":{"kind":"not_found","code":-32023,"message":"NotFound","data":{"kind":"event"}}}`,
		},
		{
			"resource_exhausted with max",
			ResourceExhausted(LimitSubscriptions, 100, 0),
			`{"mcp_error":{"kind":"resource_exhausted","code":-32013,"message":"ResourceExhausted","data":{"limit":"subscriptions","max":100}}}`,
			`{"mcp_error":{"kind":"resource_exhausted","code":-32025,"message":"ResourceExhausted","data":{"limit":"subscriptions","max":100}}}`,
		},
		{
			"resource_exhausted with retry",
			ResourceExhausted(LimitVerificationRate, 10, 1500*time.Millisecond),
			`{"mcp_error":{"kind":"resource_exhausted","code":-32013,"message":"ResourceExhausted","data":{"limit":"verification_rate","max":10,"retryAfterMs":1500}}}`,
			`{"mcp_error":{"kind":"resource_exhausted","code":-32025,"message":"ResourceExhausted","data":{"limit":"verification_rate","max":10,"retryAfterMs":1500}}}`,
		},
		{
			"unsupported",
			Unsupported("deliveryMode", "push"),
			`{"mcp_error":{"kind":"unsupported","code":-32014,"message":"Unsupported","data":{"feature":"deliveryMode","value":"push"}}}`,
			`{"mcp_error":{"kind":"unsupported","code":-32026,"message":"Unsupported","data":{"feature":"deliveryMode","value":"push"}}}`,
		},
		{
			"callback_endpoint_error",
			CallbackEndpointError(ReasonChallengeFailed),
			`{"mcp_error":{"kind":"callback_endpoint_error","code":-32015,"message":"CallbackEndpointError","data":{"reason":"challenge_failed"}}}`,
			`{"mcp_error":{"kind":"callback_endpoint_error","code":-32027,"message":"CallbackEndpointError","data":{"reason":"challenge_failed"}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.err.MarshalJSON()
			require.NoError(t, err)
			assert.Equal(t, tt.sketch, string(got), "byte-exact, no HTML escaping")

			got, err = tt.err.WithProfile(CodeProfileSketch).MarshalJSON()
			require.NoError(t, err)
			assert.Equal(t, tt.sketch, string(got))

			got, err = tt.err.WithProfile(CodeProfileSEP3415).MarshalJSON()
			require.NoError(t, err)
			assert.Equal(t, tt.sep, string(got))
			assert.Empty(t, tt.err.Profile, "WithProfile copies")

			// Through encoding/json (which HTML-escapes Marshaler output),
			// by pointer and by value, e.g. inside a struct.
			got, err = json.Marshal(tt.err)
			require.NoError(t, err)
			assert.JSONEq(t, tt.sketch, string(got))
			got, err = json.Marshal(struct{ E Error }{*tt.err})
			require.NoError(t, err)
			assert.JSONEq(t, `{"E":`+tt.sketch+`}`, string(got))
		})
	}
}

func TestError_Termination(t *testing.T) {
	t.Parallel()
	assert.Equal(t, -32012, ForbiddenCode(CodeProfileSketch))
	assert.Equal(t, -32024, ForbiddenCode(CodeProfileSEP3415))
	assert.Equal(t, -32012, ForbiddenCode(""))

	assert.Equal(t, RPCError{Code: -32024, Message: "Forbidden", Data: map[string]any{"reason": "access_revoked"}}, AccessRevoked().WithProfile(CodeProfileSEP3415).RPCError())
	assert.Equal(t, RPCError{Code: -32011, Message: "NotFound", Data: map[string]any{"kind": "event"}}, EventEnded().RPCError())
	assert.Equal(t, RPCError{Code: -32026, Message: "Unsupported", Data: map[string]any{"feature": "payloadSchema", "reason": "schema_changed"}}, SchemaChanged().WithProfile(CodeProfileSEP3415).RPCError())
}

func TestError_Unsupported(t *testing.T) {
	t.Parallel()
	assert.Equal(t, map[string]any{"feature": "f", "value": nil}, Unsupported("f", map[string]any{"a": 1}).Data)
	assert.Equal(t, map[string]any{"feature": "f", "value": nil}, Unsupported("f", []any{1}).Data)
	assert.Equal(t, map[string]any{"feature": "f", "value": true}, Unsupported("f", true).Data)
	assert.Equal(t, map[string]any{"feature": "f", "value": 1.5}, Unsupported("f", 1.5).Data)
	long := Unsupported("f", "é"+string(make([]byte, 100))).Data["value"].(string)
	assert.LessOrEqual(t, len(long), 64)
}

func TestError_Is(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("validate: %w", CallbackEndpointError(ReasonTimeout))
	var mcpErr *Error
	require.True(t, errors.As(wrapped, &mcpErr))
	assert.Equal(t, KindCallbackEndpointError, mcpErr.Kind)
	assert.Contains(t, wrapped.Error(), "callback_endpoint_error")
}

func TestParseCodeProfile(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]CodeProfile{"": CodeProfileSketch, "sketch": CodeProfileSketch, "sep-3415": CodeProfileSEP3415} {
		got, err := ParseCodeProfile(in)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := ParseCodeProfile("SEP-3415")
	assert.Error(t, err)
}

func TestLastErrorCategory(t *testing.T) {
	t.Parallel()
	for code, want := range map[string]string{
		"500": ReasonHTTP5xx, "503": ReasonHTTP5xx, "599": ReasonHTTP5xx,
		"400": ReasonHTTP4xx, "404": ReasonHTTP4xx, "410": ReasonHTTP4xx, "413": ReasonHTTP4xx, "429": ReasonHTTP4xx,
		"301": ReasonHTTP4xx, "302": ReasonHTTP4xx, "399": ReasonHTTP4xx,
		"payload_too_large":   ReasonHTTP4xx,
		"timeout":             ReasonTimeout,
		"tls_error":           ReasonTLSError,
		"connection_refused":  ReasonConnectionRefused,
		"connection_reset":    ReasonConnectionRefused,
		"dns_error":           ReasonConnectionRefused,
		"address_not_allowed": ReasonConnectionRefused,
		"network_unreachable": ReasonConnectionRefused,
		"throttled":           ReasonConnectionRefused,
		"invalid_event_id":    ReasonConnectionRefused,
		"ERR":                 ReasonConnectionRefused,
		"":                    ReasonConnectionRefused,
		"200":                 ReasonConnectionRefused,
		"600":                 ReasonConnectionRefused,
		"5xx":                 ReasonConnectionRefused,
		"5000":                ReasonConnectionRefused,
	} {
		assert.Equal(t, want, LastErrorCategory(code), "code %q", code)
	}

	assert.Nil(t, AttemptLastError("success", "200"))
	got := AttemptLastError("failed", "503")
	require.NotNil(t, got)
	assert.Equal(t, ReasonHTTP5xx, *got)
	got = AttemptLastError("failed", "timeout")
	require.NotNil(t, got)
	assert.Equal(t, ReasonTimeout, *got)
}
