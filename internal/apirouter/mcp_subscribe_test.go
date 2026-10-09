package apirouter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/deliverystatus"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCP_Subscribe_Create(t *testing.T) {
	m := newMCPTest(t)
	body := subscribeBody("user_8f2c", "order.created",
		withArguments(map[string]any{"total": map[string]any{"$gte": 100}, "currency": []any{"USD", "CAD"}}),
		withURL("HTTPS://Receiver.Example.com:443/hooks/abc"),
		withBodyField("metadata", map[string]string{"plugin": "chatgpt"}),
	)
	id := subscriptionID(t, body)
	require.True(t, strings.HasPrefix(id, "sub_"))

	before := time.Now()
	resp := m.subscribe(body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &raw))
	assert.ElementsMatch(t, []string{"id", "refreshBefore", "cursor", "truncated"}, keys(raw), "no deliveryStatus on create")
	assert.JSONEq(t, `null`, string(raw["cursor"]))
	assert.JSONEq(t, `false`, string(raw["truncated"]))
	assert.JSONEq(t, `"`+id+`"`, string(raw["id"]))

	d := m.destination(id)
	require.NotNil(t, d)
	assert.Equal(t, models.DestinationTypeMCP, d.Type)
	assert.Equal(t, models.Topics{"order.created"}, d.Topics)
	assert.Equal(t, models.Config{
		"url":             "https://receiver.example.com/hooks/abc",
		"subscription_id": id,
		"principal":       "user_8f2c",
		"event":           "order.created",
		"arguments":       `{"currency":["USD","CAD"],"total":{"$gte":100}}`,
		"schema_hash":     m.catalog.Snapshot().TopicHash("order.created"),
	}, d.Config)
	assert.NotEmpty(t, d.Config["schema_hash"])
	assert.Equal(t, models.Credentials{"secret": testSecret(1)}, d.Credentials)
	assert.Equal(t, models.Filter{"data": map[string]any{
		"total":    map[string]any{"$gte": float64(100)},
		"currency": map[string]any{"$in": []any{"USD", "CAD"}},
	}}, d.Filter)
	assert.Equal(t, models.Metadata{"plugin": "chatgpt"}, d.Metadata)
	assert.Nil(t, d.DisabledAt)
	require.NotNil(t, d.ExpiresAt)
	assert.WithinDuration(t, before.Add(time.Hour), *d.ExpiresAt, 5*time.Second)

	var result apirouter.MCPSubscribeResult
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.Equal(t, mcpevents.FormatRefreshBefore(d.ExpiresAt), result.RefreshBefore)

	require.Eventually(t, func() bool {
		return len(m.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated)) == 1
	}, time.Second, 5*time.Millisecond)
	data := m.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated)[0].Data.(opevents.TenantSubscriptionUpdatedData)
	assert.Equal(t, opevents.TenantSubscriptionUpdatedData{
		TenantID:                  mcpTenant,
		Topics:                    []string{"order.created"},
		PreviousTopics:            []string{},
		DestinationsCount:         1,
		PreviousDestinationsCount: 0,
	}, data)
	assert.Equal(t, 1, m.provider.validationCount())
}

func TestMCP_Subscribe_TTL(t *testing.T) {
	cases := []struct {
		name          string
		ttl           any // omitted when nil key
		allowNoExpiry bool
		want          time.Duration // 0: no expiry
	}{
		{name: "absent grants the default", want: time.Hour},
		{name: "a number is clamped to the minimum", ttl: 1000, want: 5 * time.Minute},
		{name: "a negative number gets the minimum", ttl: -5, want: 5 * time.Minute},
		{name: "a number is clamped to the maximum", ttl: 1e12, want: 24 * time.Hour},
		{name: "a number in range is granted", ttl: 7_200_000.9, want: 2 * time.Hour},
		{name: "null without no-expiry gets the maximum", ttl: json.RawMessage("null"), want: 24 * time.Hour},
		{name: "null with no-expiry grants none", ttl: json.RawMessage("null"), allowNoExpiry: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var opts []mcpTestOption
			if tc.allowNoExpiry {
				opts = append(opts, withMCPAllowNoExpiry())
			}
			m := newMCPTest(t, opts...)
			body := subscribeBody("p1", "order.created")
			if tc.ttl != nil {
				withParam("ttlMs", tc.ttl)(body)
			}
			before := time.Now()
			resp := m.subscribe(body)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			d := m.destination(subscriptionID(t, body))

			var raw map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &raw))
			if tc.want == 0 {
				assert.Nil(t, d.ExpiresAt)
				assert.JSONEq(t, `null`, string(raw["refreshBefore"]), "explicit null for no expiry")
				return
			}
			require.NotNil(t, d.ExpiresAt)
			assert.WithinDuration(t, before.Add(tc.want), *d.ExpiresAt, 5*time.Second)
			var refreshBefore string
			require.NoError(t, json.Unmarshal(raw["refreshBefore"], &refreshBefore))
			parsed, err := time.Parse(time.RFC3339, refreshBefore)
			require.NoError(t, err)
			assert.False(t, parsed.After(*d.ExpiresAt))
			assert.True(t, strings.HasSuffix(refreshBefore, "Z"))
		})
	}

	t.Run("an unset TTL config grants the documented defaults", func(t *testing.T) {
		m := newMCPTest(t, withMCPDeps(func(d *apirouter.MCPDeps) { d.Config.TTL = mcpevents.TTLConfig{} }))
		before := time.Now()
		body := subscribeBody("p1", "order.created")
		m.mustSubscribe(body)
		assert.WithinDuration(t, before.Add(time.Hour), *m.destination(subscriptionID(t, body)).ExpiresAt, 5*time.Second)

		body = subscribeBody("p1", "order.shipped", withParam("ttlMs", 1))
		m.mustSubscribe(body)
		assert.WithinDuration(t, before.Add(5*time.Minute), *m.destination(subscriptionID(t, body)).ExpiresAt, 5*time.Second)
	})

	t.Run("ttlMs of another type is invalid_params", func(t *testing.T) {
		m := newMCPTest(t)
		resp := m.subscribe(subscribeBody("p1", "order.created", withParam("ttlMs", "3600000")))
		requireMCPError(t, resp, "invalid_params", -32602, map[string]any{"field": "ttlMs", "reason": "invalid_type"})
	})
}

func TestMCP_Subscribe_Refresh(t *testing.T) {
	m := newMCPTest(t)
	body := subscribeBody("p1", "order.created", withBodyField("metadata", map[string]string{"a": "1"}))
	id := subscriptionID(t, body)
	m.mustSubscribe(body)
	created := m.destination(id)

	t.Run("returns deliveryStatus and keeps the generation", func(t *testing.T) {
		resp := m.subscribe(subscribeBody("p1", "order.created", withSecret(testSecret(2))))
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		assert.JSONEq(t, `{"active":true,"lastDeliveryAt":null,"lastError":null}`, string(rawField(t, resp.Body.Bytes(), "deliveryStatus")))

		d := m.destination(id)
		assert.Equal(t, created.CreatedAt.UnixMilli(), d.CreatedAt.UnixMilli(), "a refresh keeps created_at")
		assert.Equal(t, models.Metadata{"a": "1"}, d.Metadata, "omitted metadata is kept")
		assert.Equal(t, testSecret(2), d.Credentials["secret"])
		assert.Equal(t, testSecret(1), d.Credentials["previous_secret"], "the provider rotates the secret")
		assert.False(t, d.ExpiresAt.Before(*created.ExpiresAt))
		assert.Empty(t, m.resumer.resumed())
	})

	t.Run("metadata and filter are replaced when sent", func(t *testing.T) {
		filter := map[string]any{"data": map[string]any{"total": map[string]any{"$lt": 5}}}
		m.mustSubscribe(subscribeBody("p1", "order.created",
			withBodyField("metadata", map[string]string{"b": "2"}),
			withBodyField("filter", filter)))
		d := m.destination(id)
		assert.Equal(t, models.Metadata{"b": "2"}, d.Metadata)
		assert.Equal(t, models.Filter{"data": map[string]any{"total": map[string]any{"$lt": float64(5)}}}, d.Filter)

		// Without filter, the refresh goes back to the generated one.
		m.mustSubscribe(subscribeBody("p1", "order.created"))
		assert.Equal(t, models.Filter{"data": map[string]any{"total": map[string]any{"$gte": float64(100)}}}, m.destination(id).Filter)

		// An empty filter object means no filter.
		m.mustSubscribe(subscribeBody("p1", "order.created", withBodyField("filter", map[string]any{})))
		assert.Nil(t, m.destination(id).Filter)
	})

	t.Run("no tenant.subscription.updated for a refresh", func(t *testing.T) {
		time.Sleep(20 * time.Millisecond)
		assert.Len(t, m.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated), 1, "only the create emitted")
	})
}

func rawField(t *testing.T, body []byte, field string) json.RawMessage {
	t.Helper()
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &raw))
	return raw[field]
}

func TestMCP_Subscribe_DeliveryStatus(t *testing.T) {
	m := newMCPTest(t)
	body := subscribeBody("p1", "order.created")
	m.mustSubscribe(body)
	createdAt := m.destination(subscriptionID(t, body)).CreatedAt

	success := createdAt.Add(time.Minute).UTC()
	cases := []struct {
		name   string
		status *deliverystatus.Status
		err    error
		want   string // "" = omitted
	}{
		{name: "no record", want: `{"active":true,"lastDeliveryAt":null,"lastError":null}`},
		{
			name:   "last attempt succeeded",
			status: &deliverystatus.Status{LastAttemptAt: success, LastStatus: models.AttemptStatusSuccess, LastCode: "200", LastSuccessAt: success},
			want:   `{"active":true,"lastDeliveryAt":"` + success.Format(time.RFC3339) + `","lastError":null}`,
		},
		{
			name:   "last attempt failed with a 5xx",
			status: &deliverystatus.Status{LastAttemptAt: success.Add(time.Minute), LastStatus: models.AttemptStatusFailed, LastCode: "503", LastSuccessAt: success},
			want:   `{"active":true,"lastDeliveryAt":"` + success.Format(time.RFC3339) + `","lastError":"http_5xx"}`,
		},
		{
			name:   "network failure, never delivered",
			status: &deliverystatus.Status{LastAttemptAt: success, LastStatus: models.AttemptStatusFailed, LastCode: "dns_error"},
			want:   `{"active":true,"lastDeliveryAt":null,"lastError":"connection_refused"}`,
		},
		{
			name:   "a record of an earlier generation is ignored",
			status: &deliverystatus.Status{LastAttemptAt: createdAt.Add(-time.Hour), LastStatus: models.AttemptStatusFailed, LastCode: "500", LastSuccessAt: createdAt.Add(-2 * time.Hour)},
			want:   `{"active":true,"lastDeliveryAt":null,"lastError":null}`,
		},
		{name: "read error omits it", err: errors.New("redis down")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m.status.set(tc.status, tc.err)
			resp := m.subscribe(body)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			got := rawField(t, resp.Body.Bytes(), "deliveryStatus")
			if tc.want == "" {
				assert.Nil(t, got)
				return
			}
			assert.JSONEq(t, tc.want, string(got))
		})
	}
}

func TestMCP_Subscribe_ReenablesDisabled(t *testing.T) {
	m := newMCPTest(t)
	body := subscribeBody("p1", "order.created")
	id := subscriptionID(t, body)
	m.mustSubscribe(body)

	ctx := t.Context()
	_, err := m.tenantStore.DisableDestination(ctx, mcpTenant, id, time.Now())
	require.NoError(t, err)
	parked, err := m.tenantStore.ParkRetry(ctx, mcpTenant, id, `{"event_id":"e1"}`, 1000, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, "parked", string(parked))

	resp := m.subscribe(body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.JSONEq(t, `{"active":false,"lastDeliveryAt":null,"lastError":null}`, string(rawField(t, resp.Body.Bytes(), "deliveryStatus")))
	assert.Nil(t, m.destination(id).DisabledAt)

	resets, disabled := m.alerts.calls()
	assert.Equal(t, []string{id}, resets)
	assert.Equal(t, []bool{true}, disabled, "failures are reset while the subscription is still disabled")

	resumed := m.resumer.resumed()
	require.Len(t, resumed, 1)
	assert.Equal(t, mcpTenant, resumed[0].tenantID)
	assert.Equal(t, id, resumed[0].destinationID)
	members, err := m.tenantStore.PopResumeMembers(ctx, mcpTenant, resumed[0].key, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{`{"event_id":"e1"}`}, members)

	// The next refresh has nothing to resume or reset.
	resp = m.subscribe(body)
	assert.JSONEq(t, `{"active":true,"lastDeliveryAt":null,"lastError":null}`, string(rawField(t, resp.Body.Bytes(), "deliveryStatus")))
	assert.Len(t, m.resumer.resumed(), 1)
	resets, _ = m.alerts.calls()
	assert.Len(t, resets, 1)
}

func TestMCP_Subscribe_ExpiredIsReplaced(t *testing.T) {
	m := newMCPTest(t)
	body := subscribeBody("p1", "order.created")
	id := subscriptionID(t, body)
	expired := mcpDestination(id, "p1", "order.created")
	expired.CreatedAt = time.Now().Add(-2 * time.Hour)
	past := time.Now().Add(-time.Minute)
	expired.ExpiresAt = &past
	m.putDestination(expired)

	resp := m.subscribe(body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Nil(t, rawField(t, resp.Body.Bytes(), "deliveryStatus"), "a new subscription, not a refresh")
	d := m.destination(id)
	assert.True(t, d.CreatedAt.After(expired.CreatedAt), "a new generation")
	assert.True(t, d.ExpiresAt.After(time.Now()))

	require.Eventually(t, func() bool {
		return len(m.emitter.byTopic(apirouter.TopicMCPSubscriptionExpired)) == 1
	}, time.Second, 5*time.Millisecond)
	event := m.emitter.byTopic(apirouter.TopicMCPSubscriptionExpired)[0]
	assert.Equal(t, mcpTenant, event.TenantID)
	assert.Equal(t, apirouter.MCPSubscriptionExpiredData{
		TenantID:       mcpTenant,
		SubscriptionID: id,
		Principal:      "p1",
		Topic:          "order.created",
		URL:            testCallbackURL,
		ExpiresAt:      past.UTC(),
	}, event.Data)
}

func TestMCP_Subscribe_Errors(t *testing.T) {
	const invalidParams = -32602
	type tc struct {
		name string
		body any
		kind string
		code int
		data map[string]any
	}
	cases := []tc{
		{"principal missing", map[string]any{"params": subscribeBody("p", "order.created")["params"]}, "invalid_params", invalidParams, map[string]any{"field": "principal", "reason": "required"}},
		{"principal empty", subscribeBody("", "order.created"), "invalid_params", invalidParams, map[string]any{"field": "principal", "reason": "required"}},
		{"principal not a string", subscribeBody("p", "order.created", withBodyField("principal", 42)), "invalid_params", invalidParams, map[string]any{"field": "principal", "reason": "invalid_type"}},
		{"principal too long", subscribeBody(strings.Repeat("p", 513), "order.created"), "invalid_params", invalidParams, map[string]any{"field": "principal", "reason": "too_large"}},
		{"params missing", map[string]any{"principal": "p"}, "invalid_params", invalidParams, map[string]any{"field": "params", "reason": "required"}},
		{"params not an object", map[string]any{"principal": "p", "params": "x"}, "invalid_params", invalidParams, map[string]any{"field": "params", "reason": "invalid_type"}},
		{"name missing", subscribeBody("p", ""), "invalid_params", invalidParams, map[string]any{"field": "name", "reason": "required"}},
		{"unknown event", subscribeBody("p", "nope"), "not_found", -32011, map[string]any{"kind": "event"}},
		{"event without MCP", subscribeBody("p", "user.created"), "not_found", -32011, map[string]any{"kind": "event"}},
		{"event outside allowed_topics", subscribeBody("p", "order.created", withBodyField("allowed_topics", []string{"order.shipped"})), "not_found", -32011, map[string]any{"kind": "event"}},
		{"empty allowed_topics", subscribeBody("p", "order.created", withBodyField("allowed_topics", []string{})), "not_found", -32011, map[string]any{"kind": "event"}},
		{"allowed_topics not a list", subscribeBody("p", "order.created", withBodyField("allowed_topics", "order.created")), "invalid_params", invalidParams, map[string]any{"field": "allowed_topics", "reason": "invalid_type"}},
		{"delivery mode", subscribeBody("p", "order.created", func(b map[string]any) {
			b["params"].(map[string]any)["delivery"].(map[string]any)["mode"] = "poll"
		}), "unsupported", -32014, map[string]any{"feature": "deliveryMode", "value": "poll"}},
		{"bad secret", subscribeBody("p", "order.created", withSecret("whsec_short")), "invalid_params", invalidParams, map[string]any{"field": "delivery.secret", "reason": "invalid_secret"}},
		{"url scheme", subscribeBody("p", "order.created", withURL("ftp://example.com/x")), "invalid_params", invalidParams, map[string]any{"field": "delivery.url", "reason": "https_required"}},
		{"relative url", subscribeBody("p", "order.created", withURL("/hooks")), "invalid_params", invalidParams, map[string]any{"field": "delivery.url", "reason": "invalid_url"}},
		{"url with credentials", subscribeBody("p", "order.created", withURL("https://u:p@example.com/x")), "invalid_params", invalidParams, map[string]any{"field": "delivery.url", "reason": "invalid_url"}},
		{"arguments not an object", subscribeBody("p", "order.created", withParam("arguments", []any{1})), "invalid_params", invalidParams, map[string]any{"field": "arguments", "reason": "invalid_type"}},
		{"arguments against the inputSchema", subscribeBody("p", "order.created", withArguments(map[string]any{"total": "x"})), "invalid_params", invalidParams, nil},
		{"filter not an object", subscribeBody("p", "order.created", withBodyField("filter", []any{1})), "invalid_params", invalidParams, map[string]any{"field": "filter", "reason": "invalid_type"}},
		{"filter too large", subscribeBody("p", "order.created", withBodyField("filter", map[string]any{"data": strings.Repeat("x", 9000)})), "invalid_params", invalidParams, map[string]any{"field": "filter", "reason": "too_large"}},
		{"metadata of non-strings", subscribeBody("p", "order.created", withBodyField("metadata", map[string]any{"a": 1})), "invalid_params", invalidParams, map[string]any{"field": "metadata", "reason": "invalid_type"}},
	}
	m := newMCPTest(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := m.subscribe(c.body)
			if c.data == nil {
				require.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
				var parsed mcpErrorBody
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &parsed))
				assert.Equal(t, c.kind, parsed.MCPError.Kind)
				assert.Equal(t, "arguments", parsed.MCPError.Data["field"])
				assert.Equal(t, "invalid", parsed.MCPError.Data["reason"])
				assert.NotEmpty(t, parsed.MCPError.Data["errors"])
				assert.NotContains(t, resp.Body.String(), `"x"`, "no submitted values in errors")
				return
			}
			requireMCPError(t, resp, c.kind, c.code, c.data)
		})
	}
	list, err := m.tenantStore.ListDestination(t.Context(), listAll(mcpTenant))
	require.NoError(t, err)
	assert.Empty(t, list, "no failed subscribe stored anything")

	t.Run("missing or deleted tenant", func(t *testing.T) {
		resp := m.do(m.withAPIKey(m.jsonReq(http.MethodPut, "/api/v2/tenants/missing/mcp/subscriptions", subscribeBody("p", "order.created"))))
		requireMCPError(t, resp, "not_found", -32011, map[string]any{"kind": "tenant"})

		require.NoError(t, m.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("gone"))))
		require.NoError(t, m.tenantStore.DeleteTenant(t.Context(), "gone"))
		resp = m.do(m.withAPIKey(m.jsonReq(http.MethodPut, "/api/v2/tenants/gone/mcp/subscriptions", subscribeBody("p", "order.created"))))
		requireMCPError(t, resp, "not_found", -32011, map[string]any{"kind": "tenant"})
	})

	t.Run("the ID of another destination type is a conflict", func(t *testing.T) {
		body := subscribeBody("squat", "order.created")
		id := subscriptionID(t, body)
		m.putDestination(models.Destination{ID: id, Type: "webhook", Topics: models.Topics{"*"}, Config: models.Config{"url": "https://example.com"}})
		validations := m.provider.validationCount()

		resp := m.subscribe(body)
		requireMCPError(t, resp, "invalid_params", invalidParams, map[string]any{"field": "id", "reason": "conflict"})
		assert.Equal(t, validations, m.provider.validationCount(), "no verification for a conflicting ID")
		assert.Equal(t, "webhook", m.destination(id).Type)
	})

	t.Run("verification failure", func(t *testing.T) {
		m.provider.setVerify(func(context.Context, *models.Destination) error {
			return mcpValidationErr(mcpevents.CallbackEndpointError(mcpevents.ReasonChallengeFailed))
		})
		defer m.provider.setVerify(nil)
		resp := m.subscribe(subscribeBody("p", "order.created"))
		requireMCPError(t, resp, "callback_endpoint_error", -32015, map[string]any{"reason": "challenge_failed"})
	})

	t.Run("a validation error without an mcp_error is invalid_params", func(t *testing.T) {
		m.provider.setVerify(func(context.Context, *models.Destination) error {
			return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{Field: "config.url", Type: "pattern"}})
		})
		defer m.provider.setVerify(nil)
		resp := m.subscribe(subscribeBody("p", "order.created"))
		requireMCPError(t, resp, "invalid_params", invalidParams, map[string]any{"field": "delivery.url", "reason": "pattern"})
	})

	t.Run("a provider error that isn't a validation error is internal", func(t *testing.T) {
		// The registry turns it into a root validation error.
		m.provider.setVerify(func(context.Context, *models.Destination) error { return errors.New("boom") })
		defer m.provider.setVerify(nil)
		resp := m.subscribe(subscribeBody("p", "order.created"))
		testutil.RequireErrorResponse(t, resp, http.StatusInternalServerError, "internal server error")
	})

	t.Run("a missing mcp provider is internal", func(t *testing.T) {
		m := newMCPTest(t, withMCPAPIOptions(withDestRegistry(newWebhookOnlyRegistry(t))))
		resp := m.subscribe(subscribeBody("p", "order.created"))
		testutil.RequireErrorResponse(t, resp, http.StatusInternalServerError, "internal server error")
	})

	t.Run("a body that isn't a JSON object is a standard error", func(t *testing.T) {
		req := m.withAPIKey(m.jsonReq(http.MethodPut, "/api/v2/tenants/t1/mcp/subscriptions", nil))
		req.Body = http.NoBody
		resp := m.do(req)
		testutil.RequireErrorResponse(t, resp, http.StatusUnprocessableEntity, "invalid JSON")

		resp = m.subscribe([]any{1})
		testutil.RequireErrorResponse(t, resp, http.StatusUnprocessableEntity, "invalid JSON")

		resp = m.subscribe(map[string]any{"principal": "p", "params": strings.Repeat("x", 2<<20)})
		testutil.RequireErrorResponse(t, resp, http.StatusRequestEntityTooLarge, "request body too large")
	})
}

func TestMCP_Subscribe_Limits(t *testing.T) {
	t.Run("per tenant", func(t *testing.T) {
		m := newMCPTest(t, withMCPLimits(2, 0))
		m.mustSubscribe(subscribeBody("a", "order.created"))
		m.mustSubscribe(subscribeBody("b", "order.created"))
		resp := m.subscribe(subscribeBody("c", "order.created"))
		requireMCPError(t, resp, "resource_exhausted", -32013, map[string]any{"limit": "subscriptions", "max": float64(2)})

		// A refresh at the limit still works.
		m.mustSubscribe(subscribeBody("a", "order.created"))
	})

	t.Run("per principal", func(t *testing.T) {
		m := newMCPTest(t, withMCPLimits(10, 1))
		m.mustSubscribe(subscribeBody("a", "order.created"))
		resp := m.subscribe(subscribeBody("a", "order.shipped"))
		requireMCPError(t, resp, "resource_exhausted", -32013, map[string]any{"limit": "principal_subscriptions", "max": float64(1)})
		m.mustSubscribe(subscribeBody("b", "order.shipped"))
		m.mustSubscribe(subscribeBody("a", "order.created"))

		// Unsubscribing frees the slot.
		resp = m.unsubscribe(mcpTenant, subscribeBody("a", "order.created"))
		require.Equal(t, http.StatusOK, resp.Code)
		m.mustSubscribe(subscribeBody("a", "order.shipped"))
	})

	t.Run("generic destinations don't count", func(t *testing.T) {
		m := newMCPTest(t, withMCPLimits(1, 0))
		m.putDestination(models.Destination{ID: "des_1", Type: "webhook", Topics: models.Topics{"*"}, Config: models.Config{"url": "https://example.com"}})
		m.mustSubscribe(subscribeBody("a", "order.created"))
	})
}

// Every MCPDeps interface is optional: without them the endpoints work and
// skip the side effect.
func TestMCP_OptionalDeps(t *testing.T) {
	m := newMCPTest(t, withMCPDeps(func(d *apirouter.MCPDeps) {
		*d = apirouter.MCPDeps{Config: d.Config}
	}))
	body := subscribeBody("p1", "order.created")
	id := subscriptionID(t, body)
	m.mustSubscribe(body)

	_, err := m.tenantStore.DisableDestination(t.Context(), mcpTenant, id, time.Now())
	require.NoError(t, err)
	resp := m.subscribe(body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.JSONEq(t, `{"active":false,"lastDeliveryAt":null,"lastError":null}`, string(rawField(t, resp.Body.Bytes(), "deliveryStatus")))
	assert.Nil(t, m.destination(id).DisabledAt)

	resp = m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions/"+id, nil)))
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Empty(t, m.notifier.sent())
	assert.Empty(t, m.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated))
}

func TestMCP_Subscribe_CatalogWithoutMCP(t *testing.T) {
	m := newMCPTest(t, withMCPCatalog(topicschema.EmptyCatalog(mcpTopics)))
	resp := m.subscribe(subscribeBody("p", "order.created"))
	requireMCPError(t, resp, "not_found", -32011, map[string]any{"kind": "event"})
}
