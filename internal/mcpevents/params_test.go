package mcpevents

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func subscribeJSON(extra string) json.RawMessage {
	return json.RawMessage(`{"name":"user.created","delivery":{"mode":"webhook","url":"https://Example.com:443/hook","secret":"` + testSecret + `"}` + extra + `}`)
}

func TestParseSubscribeParams(t *testing.T) {
	t.Parallel()
	p, err := ParseSubscribeParams(subscribeJSON(`,"arguments":{"b":1,"a":"x"},"cursor":"c","maxAgeMs":5,"_meta":{}`))
	require.NoError(t, err)
	assert.Equal(t, "user.created", p.Name)
	assert.Equal(t, "https://example.com/hook", p.URL)
	assert.JSONEq(t, `{"a":"x","b":1}`, string(p.Arguments))
	assert.Equal(t, `{"a":"x","b":1}`, string(p.Arguments), "canonical form")
	assert.Equal(t, TTLAbsent, p.TTL.Kind)

	p, err = ParseSubscribeParams(subscribeJSON(""))
	require.NoError(t, err)
	assert.Equal(t, "{}", string(p.Arguments))
}

func TestParseSubscribeParamsErrors(t *testing.T) {
	t.Parallel()
	secret := testSecret
	tests := []struct {
		name          string
		raw           string
		field, reason string
	}{
		{"empty", ``, FieldParams, ReasonRequired},
		{"not json", `{`, FieldParams, ReasonInvalid},
		{"not an object", `[]`, FieldParams, ReasonInvalidType},
		{"duplicate key", `{"name":"a","name":"b"}`, FieldParams, ReasonInvalid},
		{"missing name", `{"delivery":{}}`, FieldName, ReasonRequired},
		{"name type", `{"name":1}`, FieldName, ReasonInvalidType},
		{"arguments type", `{"name":"a","arguments":[]}`, FieldArguments, ReasonInvalidType},
		{"missing delivery", `{"name":"a"}`, FieldDelivery, ReasonRequired},
		{"delivery type", `{"name":"a","delivery":"x"}`, FieldDelivery, ReasonInvalidType},
		{"mode type", `{"name":"a","delivery":{"mode":1,"url":"https://e.com/x","secret":"` + secret + `"}}`, FieldDeliveryMode, ReasonInvalid},
		{"missing url", `{"name":"a","delivery":{"mode":"webhook","secret":"` + secret + `"}}`, FieldURL, ReasonInvalidURL},
		{"non-https scheme", `{"name":"a","delivery":{"url":"ftp://e.com/x","secret":"` + secret + `"}}`, FieldURL, ReasonHTTPSRequired},
		{"missing secret", `{"name":"a","delivery":{"url":"https://e.com/x"}}`, FieldSecret, ReasonInvalidSecret},
		{"short secret", `{"name":"a","delivery":{"url":"https://e.com/x","secret":"whsec_AAAA"}}`, FieldSecret, ReasonInvalidSecret},
		{"no prefix", `{"name":"a","delivery":{"url":"https://e.com/x","secret":"abc"}}`, FieldSecret, ReasonInvalidSecret},
		{"ttl type", `{"name":"a","ttlMs":"1","delivery":{"url":"https://e.com/x","secret":"` + secret + `"}}`, FieldTTLMs, ReasonInvalidType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSubscribeParams(json.RawMessage(tt.raw))
			var e *Error
			require.ErrorAs(t, err, &e)
			assert.Equal(t, KindInvalidParams, e.Kind)
			assert.Equal(t, map[string]any{"field": tt.field, "reason": tt.reason}, e.Data)
		})
	}
}

func TestParseSubscribeParamsUnsupportedMode(t *testing.T) {
	t.Parallel()
	_, err := ParseSubscribeParams(json.RawMessage(`{"name":"a","delivery":{"mode":"push","url":"https://e.com/x","secret":"` + testSecret + `"}}`))
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, KindUnsupported, e.Kind)
	assert.Equal(t, -32026, e.Code())
	assert.Equal(t, map[string]any{"feature": "deliveryMode", "value": "push"}, e.Data)
}

func TestParseTTL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		extra string
		want  TTLRequest
	}{
		{``, TTLRequest{Kind: TTLAbsent}},
		{`,"ttlMs":null`, TTLRequest{Kind: TTLNull}},
		{`,"ttlMs":1500`, TTLRequest{Kind: TTLFinite, Ms: 1500}},
		{`,"ttlMs":1e999`, TTLRequest{Kind: TTLFinite, Ms: math.MaxFloat64}},
		{`,"ttlMs":-5`, TTLRequest{Kind: TTLFinite, Ms: -5}},
	}
	for _, tt := range tests {
		p, err := ParseSubscribeParams(subscribeJSON(tt.extra))
		require.NoError(t, err, tt.extra)
		assert.Equal(t, tt.want, p.TTL, tt.extra)
	}
}

func TestParseUnsubscribeParams(t *testing.T) {
	t.Parallel()
	p, err := ParseUnsubscribeParams(json.RawMessage(`{"name":"a","arguments":{"x":1},"delivery":{"url":"https://E.com/x"}}`))
	require.NoError(t, err)
	assert.Equal(t, &UnsubscribeParams{Name: "a", Arguments: json.RawMessage(`{"x":1}`), URL: "https://e.com/x"}, p)

	_, err = ParseUnsubscribeParams(json.RawMessage(`{"name":"a"}`))
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, KindInvalidParams, e.Kind)
}

func TestGrantTTL(t *testing.T) {
	t.Parallel()
	cfg := TTLConfig{Default: time.Hour, Min: 5 * time.Minute, Max: 24 * time.Hour}
	now := time.Date(2026, 1, 2, 3, 4, 5, 678_901_234, time.UTC)
	grant := func(req TTLRequest) time.Duration { return GrantTTL(req, cfg, now).Sub(now.Truncate(time.Millisecond)) }

	assert.Equal(t, time.Hour, grant(TTLRequest{Kind: TTLAbsent}))
	assert.Equal(t, 5*time.Minute, grant(TTLRequest{Kind: TTLFinite, Ms: 1}))
	assert.Equal(t, 5*time.Minute, grant(TTLRequest{Kind: TTLFinite, Ms: -1e9}))
	assert.Equal(t, 24*time.Hour, grant(TTLRequest{Kind: TTLFinite, Ms: math.MaxFloat64}))
	assert.Equal(t, 10*time.Minute+500*time.Millisecond, grant(TTLRequest{Kind: TTLFinite, Ms: 600_500.9}))
	assert.Equal(t, 24*time.Hour, grant(TTLRequest{Kind: TTLNull}), "no expiry still gets a finite grant")
	assert.Equal(t, "2026-01-02T04:04:05Z", FormatRefreshBefore(GrantTTL(TTLRequest{}, cfg, now)))
}

func TestErrorRPCError(t *testing.T) {
	t.Parallel()
	assert.Equal(t, map[string]any{"code": -32027, "message": "CallbackEndpointError", "data": map[string]any{"reason": "timeout"}},
		CallbackEndpointError(ReasonTimeout).RPCError())
	assert.Equal(t, map[string]any{"code": -32024, "message": "Forbidden"}, Forbidden(ReasonUnauthenticated).RPCError())
	assert.Equal(t, map[string]any{"code": -32602, "message": "InvalidParams"}, InvalidParams(FieldURL, ReasonHTTPSRequired).RPCError())
	assert.Equal(t, map[string]any{"code": -32023, "message": "NotFound", "data": map[string]any{"kind": "event"}}, NotFound(NotFoundEvent).RPCError())
	assert.Equal(t, map[string]any{"code": -32026, "message": "Unsupported", "data": map[string]any{"feature": "deliveryMode", "value": "poll"}},
		Unsupported("deliveryMode", "poll").RPCError())
	assert.Equal(t, map[string]any{"code": -32025, "message": "ResourceExhausted", "data": map[string]any{"limit": "subscriptions", "max": 5}},
		ResourceExhausted(LimitSubscriptions, 5, 0).RPCError())
	assert.Equal(t, map[string]any{"code": -32025, "message": "ResourceExhausted", "data": map[string]any{"limit": "verification_rate", "max": 10}},
		ResourceExhausted(LimitVerificationRate, 10, time.Second).RPCError(), "retryAfterMs stays internal")
}
