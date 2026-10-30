package apirouter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/tenantstore/memtenantstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var mcpTTLRules = mcpevents.TTLConfig{Default: time.Hour, Min: 5 * time.Minute, Max: 24 * time.Hour}

const (
	mcpSubscribePath   = "/api/v1/tenants/t1/mcp/subscribe"
	mcpUnsubscribePath = "/api/v1/tenants/t1/mcp/unsubscribe"
)

func newSubscribeTest(t *testing.T, verifier *stubVerifier, extra ...apiTestOption) *apiTest {
	t.Helper()
	return newMCPAPITest(t, verifier, append([]apiTestOption{withMCPSubscribe(mcpTTLRules)}, extra...)...)
}

type discardEmitter struct{}

func (discardEmitter) Emit(context.Context, opevents.Event) error { return nil }

func deliveryParams(url, secret string) map[string]any {
	return map[string]any{"mode": "webhook", "url": url, "secret": secret}
}

func subscribeParams(mutate ...func(map[string]any)) map[string]any {
	p := map[string]any{
		"name":      "user.created",
		"arguments": map[string]any{"status": "paid"},
		"delivery":  deliveryParams(mcpCallbackURL, mcpSecret(1)),
	}
	for _, m := range mutate {
		m(p)
	}
	return p
}

func subscribeBody(principal string, params map[string]any, allowed ...string) map[string]any {
	b := map[string]any{"principal": principal, "params": params}
	if allowed != nil {
		b["allowed_topics"] = allowed
	}
	return b
}

func postMCP(h *apiTest, path string, body any) *httptest.ResponseRecorder {
	return h.do(h.withAPIKey(h.jsonReq(http.MethodPost, path, body)))
}

type subscribeResult struct {
	ResultType     string  `json:"resultType"`
	ID             string  `json:"id"`
	RefreshBefore  string  `json:"refreshBefore"`
	Cursor         *string `json:"cursor"`
	Truncated      bool    `json:"truncated"`
	DeliveryStatus *struct {
		Active bool `json:"active"`
	} `json:"deliveryStatus"`
}

func requireSubscribed(t *testing.T, resp *httptest.ResponseRecorder) subscribeResult {
	t.Helper()
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var r subscribeResult
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &r))
	require.Equal(t, "complete", r.ResultType)
	return r
}

type mcpErrorBody struct {
	MCPError struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	} `json:"mcp_error"`
}

func requireMCPError(t *testing.T, resp *httptest.ResponseRecorder, code int, message string, data map[string]any) {
	t.Helper()
	require.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
	var b mcpErrorBody
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &b))
	assert.Equal(t, code, b.MCPError.Code)
	assert.Equal(t, message, b.MCPError.Message)
	assert.Equal(t, data, b.MCPError.Data)
}

func storedSubscription(t *testing.T, h *apiTest, id string) *models.Destination {
	t.Helper()
	d, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", id)
	if !errors.Is(err, tenantstore.ErrDestinationDeleted) {
		require.NoError(t, err)
	}
	return d
}

func mcpDestinationCount(t *testing.T, h *apiTest) int {
	t.Helper()
	list, err := h.tenantStore.ListDestination(t.Context(), tenantstore.ListDestinationRequest{TenantID: "t1", Type: []string{"mcp"}})
	require.NoError(t, err)
	return len(list)
}

func TestAPI_MCPSubscribe(t *testing.T) {
	t.Run("new subscription", func(t *testing.T) {
		v := &stubVerifier{}
		h := newSubscribeTest(t, v)
		before := time.Now()

		r := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))

		_, normalized, err := mcpevents.NormalizeCallbackURL(mcpCallbackURL)
		require.NoError(t, err)
		assert.Equal(t, mcpevents.DeriveSubscriptionID("user-1", normalized, "user.created", json.RawMessage(`{"status":"paid"}`)), r.ID)
		assert.Nil(t, r.Cursor)
		assert.False(t, r.Truncated)
		assert.Nil(t, r.DeliveryStatus, "only a refresh reports delivery status")
		refresh, err := time.Parse(time.RFC3339, r.RefreshBefore)
		require.NoError(t, err)
		assert.WithinDuration(t, before.Add(time.Hour), refresh, 2*time.Second)

		d := storedSubscription(t, h, r.ID)
		require.NotNil(t, d)
		assert.Equal(t, "mcp", d.Type)
		assert.Equal(t, models.Topics{"user.created"}, d.Topics)
		assert.Equal(t, map[string]string{"url": mcpCallbackURL, "principal": "user-1", "arguments": `{"status":"paid"}`}, map[string]string(d.Config))
		assert.Equal(t, mcpSecret(1), d.Credentials["secret"])
		require.NotNil(t, d.ExpiresAt)
		assert.False(t, d.ExpiresAt.Before(refresh), "refreshBefore is never later than the expiry")
		assert.Nil(t, d.DisabledAt)
		require.Len(t, v.Requests(), 1)
		assert.Equal(t, r.ID, v.Requests()[0].SubscriptionID)
		assert.Equal(t, "user-1", v.Requests()[0].Principal)
	})

	t.Run("without arguments", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		r := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams(func(p map[string]any) { delete(p, "arguments") }))))
		assert.NotContains(t, storedSubscription(t, h, r.ID).Config, "arguments")
	})

	t.Run("refresh keeps the id, rotates the secret and extends the expiry", func(t *testing.T) {
		v := &stubVerifier{}
		h := newSubscribeTest(t, v)
		first := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams(func(p map[string]any) { p["ttlMs"] = 600_000 }))))
		firstExpiry := *storedSubscription(t, h, first.ID).ExpiresAt

		params := subscribeParams(func(p map[string]any) {
			p["delivery"] = deliveryParams(mcpCallbackURL, mcpSecret(2))
		})
		second := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", params)))

		assert.Equal(t, first.ID, second.ID)
		require.NotNil(t, second.DeliveryStatus)
		assert.True(t, second.DeliveryStatus.Active)
		d := storedSubscription(t, h, second.ID)
		assert.Equal(t, mcpSecret(2), d.Credentials["secret"])
		assert.Equal(t, mcpSecret(1), d.Credentials[destmcp.CredentialPreviousSecret], "the old secret still signs during the grace period")
		assert.True(t, d.ExpiresAt.After(firstExpiry))
		assert.Equal(t, 1, mcpDestinationCount(t, h))
	})

	t.Run("subscribing after expiry starts a fresh subscription", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		first := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		expired := storedSubscription(t, h, first.ID)
		past := time.Now().Add(-time.Minute)
		expired.ExpiresAt = &past
		require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), *expired))

		second := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams(func(p map[string]any) {
			p["delivery"] = deliveryParams(mcpCallbackURL, mcpSecret(2))
		}))))

		assert.Equal(t, first.ID, second.ID)
		assert.Nil(t, second.DeliveryStatus)
		d := storedSubscription(t, h, second.ID)
		assert.True(t, d.ExpiresAt.After(time.Now()))
		assert.Equal(t, mcpSecret(2), d.Credentials[destmcp.CredentialSecret])
		assert.Empty(t, d.Credentials[destmcp.CredentialPreviousSecret], "the lapsed subscription's secret is not carried over")
		assert.Equal(t, 1, mcpDestinationCount(t, h))
	})

	t.Run("refresh reports a disabled subscription and re-enables it", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		first := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		disabled := storedSubscription(t, h, first.ID)
		now := time.Now()
		disabled.DisabledAt = &now
		require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), *disabled))

		second := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		require.NotNil(t, second.DeliveryStatus)
		assert.False(t, second.DeliveryStatus.Active)
		assert.Nil(t, storedSubscription(t, h, first.ID).DisabledAt)

		third := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		assert.True(t, third.DeliveryStatus.Active)
	})

	t.Run("ttl grants", func(t *testing.T) {
		tests := []struct {
			name string
			ttl  any
			omit bool
			want time.Duration
		}{
			{"omitted", nil, true, time.Hour},
			{"below min", 1, false, 5 * time.Minute},
			{"above max", int64(7 * 24 * 3600 * 1000), false, 24 * time.Hour},
			{"kept", 900_000, false, 15 * time.Minute},
			{"null", nil, false, 24 * time.Hour},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := newSubscribeTest(t, &stubVerifier{})
				before := time.Now()
				r := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams(func(p map[string]any) {
					if !tt.omit {
						p["ttlMs"] = tt.ttl
					}
				}))))
				assert.WithinDuration(t, before.Add(tt.want), *storedSubscription(t, h, r.ID).ExpiresAt, 2*time.Second)
			})
		}
	})

	t.Run("a cached verification is not repeated", func(t *testing.T) {
		v := &stubVerifier{verified: true}
		h := newSubscribeTest(t, v)
		requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		assert.Empty(t, v.Requests())
	})

	t.Run("concurrent subscribes of one key leave one destination", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{}, withSubscriptionEmitter(discardEmitter{}))
		const n = 16
		ids := make([]string, n)
		codes := make([]int, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp := postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams()))
				codes[i] = resp.Code
				var r subscribeResult
				_ = json.Unmarshal(resp.Body.Bytes(), &r)
				ids[i] = r.ID
			}()
		}
		wg.Wait()

		assert.Equal(t, 1, mcpDestinationCount(t, h))
		for i := range n {
			assert.Equal(t, http.StatusOK, codes[i])
			assert.NotEmpty(t, ids[i])
			assert.Equal(t, ids[0], ids[i])
		}
	})

	t.Run("different principals get different subscriptions", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		a := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		b := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-2", subscribeParams())))
		assert.NotEqual(t, a.ID, b.ID)
		assert.Equal(t, 2, mcpDestinationCount(t, h))
	})
}

func TestAPI_MCPSubscribeErrors(t *testing.T) {
	url := func(u string) func(map[string]any) {
		return func(p map[string]any) { p["delivery"] = deliveryParams(u, mcpSecret(1)) }
	}
	tests := []struct {
		name    string
		body    map[string]any
		verr    error
		code    int
		message string
		data    map[string]any
	}{
		{"plain http to a public address", subscribeBody("u", subscribeParams(url("http://93.184.215.14/x"))), nil,
			-32602, "InvalidParams", nil},
		{"link-local address", subscribeBody("u", subscribeParams(url("https://169.254.169.254/x"))), nil,
			-32602, "InvalidParams", nil},
		{"secret without prefix", subscribeBody("u", subscribeParams(func(p map[string]any) {
			p["delivery"] = deliveryParams(mcpCallbackURL, "abc")
		})), nil, -32602, "InvalidParams", nil},
		{"poll mode", subscribeBody("u", subscribeParams(func(p map[string]any) {
			p["delivery"] = map[string]any{"mode": "poll", "url": mcpCallbackURL, "secret": mcpSecret(1)}
		})), nil, -32026, "Unsupported", map[string]any{"feature": "deliveryMode", "value": "poll"}},
		{"oversized arguments", subscribeBody("u", subscribeParams(func(p map[string]any) {
			p["arguments"] = map[string]any{"status": strings.Repeat("a", 5<<10)}
		})), nil, -32602, "InvalidParams", nil},
		{"missing name", subscribeBody("u", subscribeParams(func(p map[string]any) { delete(p, "name") })), nil,
			-32602, "InvalidParams", nil},
		{"missing params", map[string]any{"principal": "u"}, nil,
			-32602, "InvalidParams", nil},
		{"unknown event", subscribeBody("u", subscribeParams(func(p map[string]any) { p["name"] = "order.unknown" })), nil,
			-32023, "NotFound", map[string]any{"kind": "event"}},
		{"wildcard event", subscribeBody("u", subscribeParams(func(p map[string]any) { p["name"] = "user.*" })), nil,
			-32023, "NotFound", map[string]any{"kind": "event"}},
		{"event outside the allowed topics", subscribeBody("u", subscribeParams(), "user.updated"), nil,
			-32024, "Forbidden", nil},
		{"no allowed topics", subscribeBody("u", subscribeParams(), []string{}...), nil,
			-32024, "Forbidden", nil},
		{"no principal", subscribeBody("", subscribeParams()), nil,
			-32024, "Forbidden", nil},
		{"verification rate limited", subscribeBody("u", subscribeParams()), mcpevents.ResourceExhausted(mcpevents.LimitVerificationRate, 10, time.Second),
			-32025, "ResourceExhausted", map[string]any{"limit": "verification_rate"}},
	}
	for _, category := range []string{"challenge_failed", "connection_refused", "timeout", "tls_error", "http_4xx", "http_5xx"} {
		tests = append(tests, struct {
			name    string
			body    map[string]any
			verr    error
			code    int
			message string
			data    map[string]any
		}{"verification " + category, subscribeBody("u", subscribeParams()), mcpevents.CallbackEndpointError(category),
			-32027, "CallbackEndpointError", map[string]any{"reason": category}})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &stubVerifier{err: tt.verr}
			h := newSubscribeTest(t, v)

			resp := postMCP(h, mcpSubscribePath, tt.body)

			requireMCPError(t, resp, tt.code, tt.message, tt.data)
			assert.Zero(t, mcpDestinationCount(t, h))
		})
	}

	t.Run("event missing from the definitions", func(t *testing.T) {
		h, v := newMCPCatalogAPITestWithVerifier(t, mcpEventsCatalog(t), withMCPSubscribe(mcpTTLRules))
		resp := postMCP(h, mcpSubscribePath, subscribeBody("u", subscribeParams(func(p map[string]any) { p["name"] = "user.deleted" })))
		requireMCPError(t, resp, -32023, "NotFound", map[string]any{"kind": "event"})
		assert.Empty(t, v.Requests())
	})

	t.Run("arguments rejected by the event schema", func(t *testing.T) {
		h, v := newMCPCatalogAPITestWithVerifier(t, mcpEventsCatalog(t), withMCPSubscribe(mcpTTLRules))
		resp := postMCP(h, mcpSubscribePath, subscribeBody("u", subscribeParams(func(p map[string]any) {
			p["arguments"] = map[string]any{"status": "refunded"}
		})))
		requireMCPError(t, resp, -32602, "InvalidParams", nil)
		assert.Empty(t, v.Requests())
	})

	t.Run("verification failure details stay out of the answer", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{err: errors.New("endpoint said: SECRET-MARKER at 10.1.2.3")})
		resp := postMCP(h, mcpSubscribePath, subscribeBody("u", subscribeParams()))
		assert.Equal(t, http.StatusInternalServerError, resp.Code)
		assert.NotContains(t, resp.Body.String(), "SECRET-MARKER")
		assert.NotContains(t, resp.Body.String(), "10.1.2.3")
		assert.Zero(t, mcpDestinationCount(t, h))
	})

	t.Run("a full tenant answers ResourceExhausted before any verification", func(t *testing.T) {
		v := &stubVerifier{}
		h := newSubscribeTest(t, v,
			withTenantStore(memtenantstore.New(memtenantstore.WithMaxDestinationsPerTenant(1))),
			withMaxDestinations(1))
		requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		require.Len(t, v.Requests(), 1)

		resp := postMCP(h, mcpSubscribePath, subscribeBody("user-2", subscribeParams()))

		requireMCPError(t, resp, -32025, "ResourceExhausted", map[string]any{"limit": "subscriptions", "max": float64(1)})
		assert.Len(t, v.Requests(), 1)

		refresh := postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams()))
		requireSubscribed(t, refresh)
	})

	t.Run("a malformed body answers InvalidParams", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		for _, body := range []string{`not json`, `{"principal":1}`, `{"principal":"u","allowed_topics":"x"}`} {
			req := h.withAPIKey(httptest.NewRequest(http.MethodPost, mcpSubscribePath, strings.NewReader(body)))
			req.Header.Set("Content-Type", "application/json")
			requireMCPError(t, h.do(req), -32602, "InvalidParams", nil)
		}
	})

	t.Run("a missing principal wins over invalid params", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		resp := postMCP(h, mcpSubscribePath, map[string]any{"params": map[string]any{"name": 1}})
		requireMCPError(t, resp, -32024, "Forbidden", nil)
	})

	t.Run("an id held by a destination of another type answers InvalidParams", func(t *testing.T) {
		v := &stubVerifier{}
		h := newSubscribeTest(t, v)
		id := mcpevents.DeriveSubscriptionID("u", mcpCallbackURL, "user.created", json.RawMessage(`{"status":"paid"}`))
		require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID(id), df.WithTenantID("t1"))))

		resp := postMCP(h, mcpSubscribePath, subscribeBody("u", subscribeParams()))

		requireMCPError(t, resp, -32602, "InvalidParams", nil)
		assert.Empty(t, v.Requests())
	})

	t.Run("a failed verification stores nothing", func(t *testing.T) {
		v := &stubVerifier{err: mcpevents.CallbackEndpointError(mcpevents.ReasonChallengeFailed)}
		h := newSubscribeTest(t, v)
		postMCP(h, mcpSubscribePath, subscribeBody("u", subscribeParams()))
		assert.Len(t, v.Requests(), 1)
		assert.Zero(t, mcpDestinationCount(t, h))
	})
}

func TestAPI_MCPUnsubscribe(t *testing.T) {
	t.Run("deletes the subscription, then reports it missing", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		r := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		body := map[string]any{"principal": "user-1", "params": map[string]any{
			"name": "user.created", "arguments": map[string]any{"status": "paid"}, "delivery": map[string]any{"url": mcpCallbackURL},
		}}

		resp := postMCP(h, mcpUnsubscribePath, body)

		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		assert.JSONEq(t, `{"resultType":"complete"}`, resp.Body.String())
		assert.Nil(t, storedSubscription(t, h, r.ID))

		again := postMCP(h, mcpUnsubscribePath, body)
		requireMCPError(t, again, -32023, "NotFound", map[string]any{"kind": "subscription"})
	})

	t.Run("another principal's subscription is not found", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		r := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		resp := postMCP(h, mcpUnsubscribePath, map[string]any{"principal": "user-2", "params": map[string]any{
			"name": "user.created", "arguments": map[string]any{"status": "paid"}, "delivery": map[string]any{"url": mcpCallbackURL},
		}})
		requireMCPError(t, resp, -32023, "NotFound", map[string]any{"kind": "subscription"})
		assert.NotNil(t, storedSubscription(t, h, r.ID))
	})

	t.Run("an expired subscription is not found", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		r := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		d := storedSubscription(t, h, r.ID)
		past := time.Now().Add(-time.Minute)
		d.ExpiresAt = &past
		require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), *d))
		resp := postMCP(h, mcpUnsubscribePath, map[string]any{"principal": "user-1", "params": map[string]any{
			"name": "user.created", "arguments": map[string]any{"status": "paid"}, "delivery": map[string]any{"url": mcpCallbackURL},
		}})
		requireMCPError(t, resp, -32023, "NotFound", map[string]any{"kind": "subscription"})
	})

	t.Run("invalid params and missing principal", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		resp := postMCP(h, mcpUnsubscribePath, map[string]any{"principal": "u", "params": map[string]any{"name": "user.created"}})
		requireMCPError(t, resp, -32602, "InvalidParams", nil)
		resp = postMCP(h, mcpUnsubscribePath, map[string]any{"params": map[string]any{
			"name": "user.created", "delivery": map[string]any{"url": mcpCallbackURL},
		}})
		requireMCPError(t, resp, -32024, "Forbidden", nil)
	})
}

func TestAPI_MCPSubscribeAccess(t *testing.T) {
	paths := []struct{ method, path string }{
		{http.MethodPost, mcpSubscribePath},
		{http.MethodPost, mcpUnsubscribePath},
	}

	t.Run("admin only", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		for _, p := range paths {
			body := subscribeBody("u", subscribeParams())
			assert.Equal(t, http.StatusUnauthorized, h.do(h.jsonReq(p.method, p.path, body)).Code, p.path)
			assert.Equal(t, http.StatusForbidden, h.do(h.withJWT(h.jsonReq(p.method, p.path, body), "t1")).Code, p.path)
		}
	})

	t.Run("unknown tenant", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		unsubscribe := h.do(h.withAPIKey(h.jsonReq(http.MethodPost, "/api/v1/tenants/missing/mcp/unsubscribe", map[string]any{
			"principal": "u", "params": map[string]any{"name": "user.created", "delivery": map[string]any{"url": mcpCallbackURL}},
		})))
		requireMCPError(t, unsubscribe, -32023, "NotFound", map[string]any{"kind": "subscription"})
		tenant, err := h.tenantStore.RetrieveTenant(t.Context(), "missing")
		require.NoError(t, err)
		assert.Nil(t, tenant)
	})

	t.Run("subscribing creates a missing tenant", func(t *testing.T) {
		h := newSubscribeTest(t, &stubVerifier{})
		resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPost, "/api/v1/tenants/fresh/mcp/subscribe", subscribeBody("u", subscribeParams()))))
		requireSubscribed(t, resp)
		tenant, err := h.tenantStore.RetrieveTenant(t.Context(), "fresh")
		require.NoError(t, err)
		require.NotNil(t, tenant)
		assert.Equal(t, 1, tenant.DestinationsCount)
	})

	t.Run("routes answer 404 while MCP is off", func(t *testing.T) {
		h := newAPITest(t)
		require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
		for _, p := range paths {
			resp := h.do(h.withAPIKey(h.jsonReq(p.method, p.path, subscribeBody("u", subscribeParams()))))
			assert.Equal(t, http.StatusNotFound, resp.Code, p.path)
		}
	})
}

func TestAPI_MCPSubscribeRaces(t *testing.T) {
	newRacing := func(t *testing.T) (*apiTest, *racingStore) {
		t.Helper()
		rs := &racingStore{TenantStore: memtenantstore.New()}
		return newSubscribeTest(t, &stubVerifier{}, withTenantStore(rs), withSubscriptionEmitter(discardEmitter{})), rs
	}
	renewed := func(t *testing.T, rs *racingStore, id string, expiresAt time.Time) {
		t.Helper()
		competingUpdate(t, rs.TenantStore, "t1", id, func(d *models.Destination) { d.ExpiresAt = &expiresAt })
	}
	once := func(hook func()) func(int) {
		var done bool
		return func(int) {
			if !done {
				done = true
				hook()
			}
		}
	}

	t.Run("a refresh that loses a write re-reads and succeeds", func(t *testing.T) {
		h, rs := newRacing(t)
		first := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		rs.afterRetrieve = once(func() { renewed(t, rs, first.ID, time.Now().Add(2*time.Hour)) })

		second := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))

		assert.Equal(t, first.ID, second.ID)
		assert.Equal(t, 1, mcpDestinationCount(t, h))
	})

	t.Run("replacing an expired subscription that was renewed meanwhile refreshes it", func(t *testing.T) {
		h, rs := newRacing(t)
		first := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		renewed(t, rs, first.ID, time.Now().Add(-time.Minute))
		rs.afterRetrieve = once(func() { renewed(t, rs, first.ID, time.Now().Add(2*time.Hour)) })

		second := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))

		assert.Equal(t, first.ID, second.ID)
		require.NotNil(t, second.DeliveryStatus, "the second pass refreshed a live subscription")
		assert.Equal(t, 1, mcpDestinationCount(t, h))
	})

	t.Run("replacing an expired subscription that was purged meanwhile creates it", func(t *testing.T) {
		h, rs := newRacing(t)
		first := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		renewed(t, rs, first.ID, time.Now().Add(-time.Minute))
		rs.afterRetrieve = once(func() { require.NoError(t, rs.TenantStore.DeleteDestination(t.Context(), "t1", first.ID)) })

		second := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))

		assert.Equal(t, first.ID, second.ID)
		assert.Nil(t, second.DeliveryStatus)
		assert.Equal(t, 1, mcpDestinationCount(t, h))
	})

	t.Run("a subscribe that keeps losing answers 503 with Retry-After and a retry succeeds", func(t *testing.T) {
		h, rs := newRacing(t)
		first := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		rs.afterRetrieve = func(int) { renewed(t, rs, first.ID, time.Now().Add(2*time.Hour)) }

		resp := postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams()))
		assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
		assert.Equal(t, "1", resp.Header().Get("Retry-After"))
		assert.Equal(t, 1, mcpDestinationCount(t, h))

		rs.stopRacing()
		second := requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		assert.Equal(t, first.ID, second.ID)
	})

	t.Run("a tenant deleted before the write answers 503 and a retry succeeds", func(t *testing.T) {
		h, rs := newRacing(t)
		rs.afterRetrieve = once(func() { require.NoError(t, rs.TenantStore.DeleteTenant(t.Context(), "t1")) })

		resp := postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams()))
		assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
		assert.Equal(t, 0, mcpDestinationCount(t, h))

		requireSubscribed(t, postMCP(h, mcpSubscribePath, subscribeBody("user-1", subscribeParams())))
		assert.Equal(t, 1, mcpDestinationCount(t, h))
	})
}
