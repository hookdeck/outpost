package mcpevents

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func secretOf(n int, enc *base64.Encoding) (string, []byte) {
	key := bytes.Repeat([]byte{0xfb, 0xff, 0xbf}, n)[:n] // encodes with '+' and '/'
	return SecretPrefix + enc.EncodeToString(key), key
}

var testSecret, testSecretKey = secretOf(32, base64.StdEncoding)

func subscribeParams(t *testing.T, mutate func(m map[string]any)) json.RawMessage {
	t.Helper()
	m := map[string]any{
		"name":      "order.created",
		"arguments": map[string]any{"total": map[string]any{"$gte": 100}, "currency": "USD"},
		"delivery": map[string]any{
			"mode":   "webhook",
			"url":    "https://receiver.example.com/mcp-events/abc123",
			"secret": testSecret,
		},
		"cursor": nil,
	}
	if mutate != nil {
		mutate(m)
	}
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	return raw
}

func delivery(m map[string]any) map[string]any { return m["delivery"].(map[string]any) }

// requireMCPError asserts err is an *Error of kind with data.
func requireMCPError(t *testing.T, err error, kind Kind, data map[string]any) {
	t.Helper()
	var mcpErr *Error
	require.True(t, errors.As(err, &mcpErr), "want *Error, got %v", err)
	assert.Equal(t, kind, mcpErr.Kind)
	assert.Equal(t, data, mcpErr.Data)
}

func TestParseSubscribeParams(t *testing.T) {
	t.Parallel()
	p, err := ParseSubscribeParams(subscribeParams(t, nil))
	require.NoError(t, err)
	assert.Equal(t, "order.created", p.Name)
	assert.JSONEq(t, `{"currency":"USD","total":{"$gte":100}}`, string(p.Arguments))
	assert.Equal(t, `{"currency":"USD","total":{"$gte":100}}`, string(p.Arguments), "canonical")
	assert.Equal(t, map[string]any{"currency": "USD", "total": map[string]any{"$gte": float64(100)}}, p.ArgumentsMap)
	assert.Equal(t, DeliveryModeWebhook, p.Delivery.Mode)
	assert.Equal(t, "https://receiver.example.com/mcp-events/abc123", p.Delivery.URLString)
	assert.Equal(t, p.Delivery.URLString, p.Delivery.URL.String())
	assert.Equal(t, testSecret, p.Delivery.Secret)
	assert.Equal(t, testSecretKey, p.Delivery.SecretKey)
	assert.Equal(t, TTLRequest{Kind: TTLAbsent}, p.TTL)

	// The derived ID matches the guide's for the same key.
	assert.Equal(t, "sub_1fa69bec3707ee16223ae5400f92f935", DeriveSubscriptionID("user_8f2c", p.Delivery.URLString, p.Name, p.Arguments))
}

func TestParseSubscribeParams_Normalizes(t *testing.T) {
	t.Parallel()
	a, err := ParseSubscribeParams(json.RawMessage(`{"name":"t","arguments":{"b":[1,2],"a":1.50},"delivery":{"url":"HTTPS://Receiver.Example.COM:0443/x/../y","secret":"` + testSecret + `"}}`))
	require.NoError(t, err)
	b, err := ParseSubscribeParams(json.RawMessage(`{"delivery":{"secret":"` + testSecret + `","url":"https://receiver.example.com./y"},"arguments":{"a":1.5,"b":[1,2]},"name":"t"}`))
	require.NoError(t, err)
	assert.Equal(t, a.Arguments, b.Arguments)
	assert.Equal(t, a.Delivery.URLString, b.Delivery.URLString)
	assert.Equal(t, "https://receiver.example.com/y", a.Delivery.URLString)
	assert.Equal(t, DeriveSubscriptionID("p", a.Delivery.URLString, a.Name, a.Arguments), DeriveSubscriptionID("p", b.Delivery.URLString, b.Name, b.Arguments))
}

func TestParseSubscribeParams_Arguments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		arguments any // json.RawMessage for raw text; "absent" to delete
		want      string
		wantErr   map[string]any
	}{
		{"absent defaults to {}", "absent", `{}`, nil},
		{"null defaults to {}", json.RawMessage(`null`), `{}`, nil},
		{"empty", json.RawMessage(`{}`), `{}`, nil},
		{"array", json.RawMessage(`[]`), "", map[string]any{"field": "arguments", "reason": ReasonInvalidType}},
		{"string", json.RawMessage(`"x"`), "", map[string]any{"field": "arguments", "reason": ReasonInvalidType}},
		{"number", json.RawMessage(`1`), "", map[string]any{"field": "arguments", "reason": ReasonInvalidType}},
		{"number out of range", json.RawMessage(`{"a":1e400}`), "", map[string]any{"field": "arguments", "reason": ReasonInvalid}},
		{"nested", json.RawMessage(`{"a":{"$lt":-0,"$gt":1E2},"b":["y","x"]}`), `{"a":{"$gt":100,"$lt":0},"b":["y","x"]}`, nil},
		{"at the size limit", json.RawMessage(`{"a":"` + strings.Repeat("x", MaxArgumentsBytes-8) + `"}`), `{"a":"` + strings.Repeat("x", MaxArgumentsBytes-8) + `"}`, nil},
		{"over the size limit", json.RawMessage(`{"a":"` + strings.Repeat("x", MaxArgumentsBytes-7) + `"}`), "", map[string]any{"field": "arguments", "reason": ReasonTooLarge}},
		{"canonical size counts, not raw", json.RawMessage(`{"a":` + strings.Repeat(" ", MaxArgumentsBytes) + `1}`), `{"a":1}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := subscribeParams(t, func(m map[string]any) {
				if tt.arguments == "absent" {
					delete(m, "arguments")
				} else {
					m["arguments"] = tt.arguments
				}
			})
			p, err := ParseSubscribeParams(raw)
			if tt.wantErr != nil {
				requireMCPError(t, err, KindInvalidParams, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(p.Arguments))
			assert.NotNil(t, p.ArgumentsMap)
		})
	}
}

func TestParseSubscribeParams_DeliveryMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode    any // "absent" to delete
		wantErr *Error
	}{
		{"absent", nil},
		{"webhook", nil},
		{"push", Unsupported("deliveryMode", "push")},
		{"poll", Unsupported("deliveryMode", "poll")},
		{"Webhook", Unsupported("deliveryMode", "Webhook")},
		{nil, Unsupported("deliveryMode", nil)},
		{json.RawMessage(`3`), Unsupported("deliveryMode", json.Number("3"))},
		{json.RawMessage(`{"x":1}`), Unsupported("deliveryMode", nil)},
		{strings.Repeat("m", 100), Unsupported("deliveryMode", strings.Repeat("m", 64))},
	}
	for _, tt := range tests {
		raw := subscribeParams(t, func(m map[string]any) {
			if tt.mode == "absent" {
				delete(delivery(m), "mode")
			} else {
				delivery(m)["mode"] = tt.mode
			}
		})
		p, err := ParseSubscribeParams(raw)
		if tt.wantErr == nil {
			require.NoError(t, err, "%v", tt.mode)
			assert.Equal(t, DeliveryModeWebhook, p.Delivery.Mode)
			continue
		}
		requireMCPError(t, err, KindUnsupported, tt.wantErr.Data)
	}
}

func TestParseSubscribeParams_Secret(t *testing.T) {
	t.Parallel()
	s23, _ := secretOf(23, base64.StdEncoding)
	s24, k24 := secretOf(24, base64.StdEncoding)
	s64, k64 := secretOf(64, base64.StdEncoding)
	s65, _ := secretOf(65, base64.StdEncoding)
	raw25, k25 := secretOf(25, base64.RawStdEncoding)
	padded25, _ := secretOf(25, base64.StdEncoding)
	raw23, _ := secretOf(23, base64.RawStdEncoding)
	raw65, _ := secretOf(65, base64.RawStdEncoding)
	urlSafe, _ := secretOf(32, base64.URLEncoding)
	rawURLSafe, _ := secretOf(32, base64.RawURLEncoding)
	require.Contains(t, urlSafe, "_", "fixture must exercise the URL-safe alphabet")

	valid := map[string][]byte{s24: k24, s64: k64, raw25: k25, padded25: k25}
	for secret, key := range valid {
		got, err := DecodeSecret(secret)
		require.NoError(t, err, secret)
		assert.Equal(t, key, got)
		p, err := ParseSubscribeParams(subscribeParams(t, func(m map[string]any) { delivery(m)["secret"] = secret }))
		require.NoError(t, err)
		assert.Equal(t, key, p.Delivery.SecretKey)
	}

	invalid := []any{
		s23, s65, raw23, raw65, urlSafe, rawURLSafe,
		strings.TrimPrefix(s24, SecretPrefix), // no prefix
		"WHSEC_" + strings.TrimPrefix(s24, SecretPrefix),
		"whsec_",
		s24 + "=",                                // extra padding
		s24[:len(s24)-1] + "=",                   // padding in the wrong place
		padded25 + "=",                           // three padding characters
		strings.TrimSuffix(padded25, "=") + "==", // wrong padding count
		s24[:20] + "\n" + s24[20:],               // encoding/base64 would skip the newline
		s24[:20] + "\r" + s24[20:],
		s24[:20] + " " + s24[20:],
		s24[:20] + "=" + s24[21:],
		s24 + "A", // length 4n+1
		" " + s24,
		nil, 42, true, map[string]any{}, []any{s24},
	}
	for _, secret := range invalid {
		raw := subscribeParams(t, func(m map[string]any) { delivery(m)["secret"] = secret })
		_, err := ParseSubscribeParams(raw)
		requireMCPError(t, err, KindInvalidParams, map[string]any{"field": FieldDeliverySecret, "reason": ReasonInvalidSecret})
		if s, ok := secret.(string); ok {
			_, err := DecodeSecret(s)
			assert.Error(t, err, "%q", s)
		}
	}
	// Absent.
	_, err := ParseSubscribeParams(subscribeParams(t, func(m map[string]any) { delete(delivery(m), "secret") }))
	requireMCPError(t, err, KindInvalidParams, map[string]any{"field": FieldDeliverySecret, "reason": ReasonInvalidSecret})

	// The error never echoes the secret.
	var mcpErr *Error
	require.ErrorAs(t, err, &mcpErr)
	body, err := json.Marshal(mcpErr)
	require.NoError(t, err)
	assert.NotContains(t, string(body), strings.TrimPrefix(s23, SecretPrefix))
}

func TestParseSubscribeParams_TTL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ttl     any // "absent" to delete
		want    TTLRequest
		wantErr bool
	}{
		{"absent", TTLRequest{Kind: TTLAbsent}, false},
		{nil, TTLRequest{Kind: TTLNull}, false},
		{json.RawMessage(`3600000`), TTLRequest{Kind: TTLFinite, Ms: 3600000}, false},
		{json.RawMessage(`0`), TTLRequest{Kind: TTLFinite, Ms: 0}, false},
		{json.RawMessage(`-5`), TTLRequest{Kind: TTLFinite, Ms: -5}, false},
		{json.RawMessage(`1.9`), TTLRequest{Kind: TTLFinite, Ms: 1.9}, false},
		{json.RawMessage(`1e3`), TTLRequest{Kind: TTLFinite, Ms: 1000}, false},
		{json.RawMessage(`1e400`), TTLRequest{Kind: TTLFinite, Ms: math.MaxFloat64}, false},
		{json.RawMessage(`-1e400`), TTLRequest{Kind: TTLFinite, Ms: -math.MaxFloat64}, false},
		{json.RawMessage(`1e-400`), TTLRequest{Kind: TTLFinite, Ms: 0}, false},
		{"3600000", TTLRequest{}, true},
		{true, TTLRequest{}, true},
		{map[string]any{}, TTLRequest{}, true},
		{[]any{1}, TTLRequest{}, true},
	}
	for _, tt := range tests {
		raw := subscribeParams(t, func(m map[string]any) {
			if tt.ttl == "absent" {
				delete(m, "ttlMs")
			} else {
				m["ttlMs"] = tt.ttl
			}
		})
		p, err := ParseSubscribeParams(raw)
		if tt.wantErr {
			requireMCPError(t, err, KindInvalidParams, map[string]any{"field": FieldTTLMs, "reason": ReasonInvalidType})
			continue
		}
		require.NoError(t, err, "%v", tt.ttl)
		assert.Equal(t, tt.want, p.TTL, "%v", tt.ttl)
	}
}

func TestParseSubscribeParams_Invalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want map[string]any
	}{
		{"empty", ``, map[string]any{"field": "params", "reason": ReasonRequired}},
		{"null", `null`, map[string]any{"field": "params", "reason": ReasonInvalidType}},
		{"array", `[]`, map[string]any{"field": "params", "reason": ReasonInvalidType}},
		{"malformed", `{"name":`, map[string]any{"field": "params", "reason": ReasonInvalid}},
		{"trailing data", `{"name":"a"} {}`, map[string]any{"field": "params", "reason": ReasonInvalid}},
		{"duplicate name", `{"name":"allowed","name":"other","delivery":{}}`, map[string]any{"field": "params", "reason": ReasonInvalid}},
		{"duplicate argument", `{"name":"a","arguments":{"x":1,"x":2}}`, map[string]any{"field": "params", "reason": ReasonInvalid}},
		{"duplicate delivery url", `{"name":"a","delivery":{"url":"https://a.example/","url":"https://b.example/"}}`, map[string]any{"field": "params", "reason": ReasonInvalid}},
		{"too deep", `{"name":"a","arguments":{"a":` + strings.Repeat(`[`, 20) + strings.Repeat(`]`, 20) + `}}`, map[string]any{"field": "params", "reason": ReasonInvalid}},
		{"too large", `{"name":"a","x":"` + strings.Repeat("x", MaxParamsBytes) + `"}`, map[string]any{"field": "params", "reason": ReasonTooLarge}},
		{"name missing", `{"delivery":{}}`, map[string]any{"field": "name", "reason": ReasonRequired}},
		{"name null", `{"name":null}`, map[string]any{"field": "name", "reason": ReasonRequired}},
		{"name empty", `{"name":""}`, map[string]any{"field": "name", "reason": ReasonRequired}},
		{"name number", `{"name":1}`, map[string]any{"field": "name", "reason": ReasonInvalidType}},
		// Keys match exactly: JavaScript servers read "name", so must we.
		{"name wrong case", `{"Name":"order.created","NAME":"x"}`, map[string]any{"field": "name", "reason": ReasonRequired}},
		{"delivery missing", `{"name":"a"}`, map[string]any{"field": "delivery", "reason": ReasonRequired}},
		{"delivery null", `{"name":"a","delivery":null}`, map[string]any{"field": "delivery", "reason": ReasonRequired}},
		{"delivery string", `{"name":"a","delivery":"https://a.example/"}`, map[string]any{"field": "delivery", "reason": ReasonInvalidType}},
		{"url missing", `{"name":"a","delivery":{"secret":"` + testSecret + `"}}`, map[string]any{"field": "delivery.url", "reason": ReasonInvalidURL}},
		{"url wrong case key", `{"name":"a","delivery":{"URL":"https://a.example/","secret":"` + testSecret + `"}}`, map[string]any{"field": "delivery.url", "reason": ReasonInvalidURL}},
		{"url number", `{"name":"a","delivery":{"url":1,"secret":"` + testSecret + `"}}`, map[string]any{"field": "delivery.url", "reason": ReasonInvalidURL}},
		{"url relative", `{"name":"a","delivery":{"url":"/x","secret":"` + testSecret + `"}}`, map[string]any{"field": "delivery.url", "reason": ReasonInvalidURL}},
		{"url credentials", `{"name":"a","delivery":{"url":"https://u:p@a.example/","secret":"` + testSecret + `"}}`, map[string]any{"field": "delivery.url", "reason": ReasonInvalidURL}},
		{"url scheme", `{"name":"a","delivery":{"url":"ftp://a.example/","secret":"` + testSecret + `"}}`, map[string]any{"field": "delivery.url", "reason": ReasonHTTPSRequired}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseSubscribeParams(json.RawMessage(tt.raw))
			requireMCPError(t, err, KindInvalidParams, tt.want)
		})
	}
}

func TestParseSubscribeParams_UnknownFieldsIgnored(t *testing.T) {
	t.Parallel()
	raw := subscribeParams(t, func(m map[string]any) {
		m["cursor"] = "cursor_123"
		m["maxAgeMs"] = 300000
		m["_meta"] = map[string]any{"progressToken": 1}
		m["future"] = []any{1, "x"}
		delivery(m)["future"] = map[string]any{"a": 1}
		delivery(m)["headers"] = map[string]any{"Authorization": "x"}
	})
	p, err := ParseSubscribeParams(raw)
	require.NoError(t, err)
	assert.Equal(t, "order.created", p.Name)

	// cursor of any type is ignored.
	for _, cursor := range []any{nil, 1, map[string]any{}, []any{}} {
		_, err := ParseSubscribeParams(subscribeParams(t, func(m map[string]any) { m["cursor"] = cursor }))
		assert.NoError(t, err)
	}
}

func TestParseUnsubscribeParams(t *testing.T) {
	t.Parallel()
	p, err := ParseUnsubscribeParams(json.RawMessage(`{"name":"order.created","arguments":{"total":{"$gte":100},"currency":"USD"},"delivery":{"url":"https://receiver.example.com:443/mcp-events/abc123"}}`))
	require.NoError(t, err)
	assert.Equal(t, "order.created", p.Name)
	assert.Equal(t, `{"currency":"USD","total":{"$gte":100}}`, string(p.Arguments))
	assert.Equal(t, "https://receiver.example.com/mcp-events/abc123", p.URLString)
	assert.Equal(t, p.URLString, p.URL.String())
	assert.Equal(t, "sub_1fa69bec3707ee16223ae5400f92f935", DeriveSubscriptionID("user_8f2c", p.URLString, p.Name, p.Arguments))

	// Mode and secret are ignored, whatever they hold; arguments default to {}.
	p, err = ParseUnsubscribeParams(json.RawMessage(`{"name":"a","delivery":{"url":"https://a.example","mode":"push","secret":"nope"},"ttlMs":"x"}`))
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(p.Arguments))

	_, err = ParseUnsubscribeParams(json.RawMessage(`{"name":"a"}`))
	requireMCPError(t, err, KindInvalidParams, map[string]any{"field": "delivery", "reason": ReasonRequired})
	_, err = ParseUnsubscribeParams(json.RawMessage(`{"name":"a","delivery":{"url":"http://[::1"}}`))
	requireMCPError(t, err, KindInvalidParams, map[string]any{"field": "delivery.url", "reason": ReasonInvalidURL})
	_, err = ParseUnsubscribeParams(json.RawMessage(`{"name":"a","arguments":[],"delivery":{"url":"https://a.example"}}`))
	requireMCPError(t, err, KindInvalidParams, map[string]any{"field": "arguments", "reason": ReasonInvalidType})
}
