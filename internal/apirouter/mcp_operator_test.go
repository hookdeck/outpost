package apirouter_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCP_Unsubscribe(t *testing.T) {
	t.Run("deletes the subscription and returns {}", func(t *testing.T) {
		m := newMCPTest(t)
		body := subscribeBody("p1", "order.created")
		id := subscriptionID(t, body)
		m.mustSubscribe(body)
		other := subscribeBody("p2", "order.created")
		m.mustSubscribe(other)
		require.Eventually(t, func() bool {
			return len(m.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated)) == 2
		}, time.Second, 5*time.Millisecond)

		// Secret and mode are ignored.
		resp := m.unsubscribe(mcpTenant, subscribeBody("p1", "order.created", withSecret("not a secret")))
		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, `{}`, resp.Body.String())
		assert.Nil(t, m.destination(id))
		assert.NotNil(t, m.destination(subscriptionID(t, other)), "other principals keep theirs")
		assert.Empty(t, m.notifier.sent(), "no terminated envelope for an unsubscribe")

		require.Eventually(t, func() bool {
			return len(m.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated)) == 3
		}, time.Second, 5*time.Millisecond)
		events := m.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated)
		assert.Equal(t, opevents.TenantSubscriptionUpdatedData{
			TenantID:                  mcpTenant,
			Topics:                    []string{"order.created"},
			PreviousTopics:            []string{"order.created"},
			DestinationsCount:         1,
			PreviousDestinationsCount: 2,
		}, events[2].Data)

		// Again: nothing matches, still {}.
		resp = m.unsubscribe(mcpTenant, body)
		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, `{}`, resp.Body.String())
	})

	t.Run("normalizes the URL and defaults the arguments like subscribe", func(t *testing.T) {
		m := newMCPTest(t)
		body := subscribeBody("p1", "order.created", withParam("arguments", nil))
		id := subscriptionID(t, body)
		m.mustSubscribe(body)

		unsubscribe := map[string]any{
			"principal": "p1",
			"params": map[string]any{
				"name":     "order.created",
				"delivery": map[string]any{"url": "https://RECEIVER.example.com:443/hooks/abc"},
			},
		}
		resp := m.unsubscribe(mcpTenant, unsubscribe)
		require.Equal(t, http.StatusOK, resp.Code)
		assert.Nil(t, m.destination(id))
	})

	t.Run("works whatever the catalog says", func(t *testing.T) {
		m := newMCPTest(t)
		body := subscribeBody("p1", "legacy.topic", withParam("arguments", map[string]any{}))
		id := subscriptionID(t, body)
		m.putDestination(mcpDestination(id, "p1", "legacy.topic"))

		resp := m.unsubscribe(mcpTenant, body)
		require.Equal(t, http.StatusOK, resp.Code)
		assert.Nil(t, m.destination(id))
	})

	t.Run("missing tenant returns {}", func(t *testing.T) {
		m := newMCPTest(t)
		resp := m.unsubscribe("missing", subscribeBody("p1", "order.created"))
		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, `{}`, resp.Body.String())
	})

	t.Run("leaves other destination types alone", func(t *testing.T) {
		m := newMCPTest(t)
		body := subscribeBody("p1", "order.created")
		id := subscriptionID(t, body)
		m.putDestination(models.Destination{ID: id, Type: "webhook", Topics: models.Topics{"*"}, Config: models.Config{"url": "https://example.com"}})

		resp := m.unsubscribe(mcpTenant, body)
		require.Equal(t, http.StatusOK, resp.Code)
		assert.NotNil(t, m.destination(id))
	})

	t.Run("shape errors are invalid_params", func(t *testing.T) {
		m := newMCPTest(t)
		cases := []struct {
			body any
			data map[string]any
		}{
			{map[string]any{"params": subscribeBody("p", "order.created")["params"]}, map[string]any{"field": "principal", "reason": "required"}},
			{map[string]any{"principal": "p"}, map[string]any{"field": "params", "reason": "required"}},
			{subscribeBody("p", ""), map[string]any{"field": "name", "reason": "required"}},
			{subscribeBody("p", "order.created", withParam("arguments", "x")), map[string]any{"field": "arguments", "reason": "invalid_type"}},
			{subscribeBody("p", "order.created", withParam("delivery", nil)), map[string]any{"field": "delivery", "reason": "required"}},
			{subscribeBody("p", "order.created", withURL("not a url")), map[string]any{"field": "delivery.url", "reason": "invalid_url"}},
		}
		for _, c := range cases {
			requireMCPError(t, m.unsubscribe(mcpTenant, c.body), "invalid_params", -32602, c.data)
		}
	})
}

func (m *mcpTest) listSubscriptions(query string) []map[string]json.RawMessage {
	m.t.Helper()
	resp := m.do(m.withAPIKey(m.jsonReq(http.MethodGet, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions"+query, nil)))
	require.Equal(m.t, http.StatusOK, resp.Code, resp.Body.String())
	var list []map[string]json.RawMessage
	require.NoError(m.t, json.Unmarshal(resp.Body.Bytes(), &list))
	return list
}

func TestMCP_ListSubscriptions(t *testing.T) {
	m := newMCPTest(t, withMCPAllowNoExpiry())
	a := subscribeBody("alice", "order.created",
		withArguments(map[string]any{"currency": "USD"}),
		withBodyField("metadata", map[string]string{"plugin": "chatgpt"}))
	m.mustSubscribe(a)
	m.mustSubscribe(subscribeBody("alice", "order.shipped", withParam("ttlMs", nil)))
	m.mustSubscribe(subscribeBody("bob", "order.created"))
	m.putDestination(models.Destination{ID: "des_webhook", Type: "webhook", Topics: models.Topics{"*"}, Config: models.Config{"url": "https://example.com"}})

	t.Run("lists every subscription with its fields", func(t *testing.T) {
		list := m.listSubscriptions("")
		require.Len(t, list, 3)
		id := subscriptionID(t, a)
		var first map[string]json.RawMessage
		for _, s := range list {
			if string(s["id"]) == `"`+id+`"` {
				first = s
			}
		}
		require.NotNil(t, first)
		assert.ElementsMatch(t, []string{"id", "principal", "event", "arguments", "url", "filter", "expires_at", "created_at", "updated_at", "disabled_at", "metadata"}, keys(first))
		assert.JSONEq(t, `"alice"`, string(first["principal"]))
		assert.JSONEq(t, `"order.created"`, string(first["event"]))
		assert.JSONEq(t, `{"currency":"USD"}`, string(first["arguments"]))
		assert.JSONEq(t, `"`+testCallbackURL+`"`, string(first["url"]))
		assert.JSONEq(t, `{"data":{"currency":"USD"}}`, string(first["filter"]))
		assert.JSONEq(t, `null`, string(first["disabled_at"]))
		assert.JSONEq(t, `{"plugin":"chatgpt"}`, string(first["metadata"]))
		assert.NotEqual(t, `null`, string(first["expires_at"]))
		assert.NotContains(t, string(first["url"]), "whsec_")
	})

	t.Run("no expiry is an explicit null", func(t *testing.T) {
		list := m.listSubscriptions("?topic=order.shipped")
		require.Len(t, list, 1)
		assert.JSONEq(t, `null`, string(list[0]["expires_at"]))
		assert.JSONEq(t, `{"total":{"$gte":100}}`, string(list[0]["arguments"]))
	})

	t.Run("filters by principal and topic", func(t *testing.T) {
		assert.Len(t, m.listSubscriptions("?principal=alice"), 2)
		assert.Len(t, m.listSubscriptions("?principal=bob&topic=order.created"), 1)
		assert.Empty(t, m.listSubscriptions("?principal=carol"))
	})

	t.Run("tenant JWT", func(t *testing.T) {
		resp := m.do(m.withJWT(m.jsonReq(http.MethodGet, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions", nil), mcpTenant))
		require.Equal(t, http.StatusOK, resp.Code)
		var list []any
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
		assert.Len(t, list, 3)
	})

	t.Run("empty list is []", func(t *testing.T) {
		require.NoError(t, m.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2"))))
		resp := m.do(m.withAPIKey(m.jsonReq(http.MethodGet, "/api/v2/tenants/t2/mcp/subscriptions", nil)))
		require.Equal(t, http.StatusOK, resp.Code)
		assert.JSONEq(t, `[]`, resp.Body.String())
	})
}

func TestMCP_DeleteSubscription(t *testing.T) {
	t.Run("revokes and sends a terminated envelope", func(t *testing.T) {
		m := newMCPTest(t, withMCPProfile(mcpevents.CodeProfileSEP3415))
		body := subscribeBody("p1", "order.created")
		id := subscriptionID(t, body)
		m.mustSubscribe(body)
		m.mustSubscribe(subscribeBody("p1", "order.created", withSecret(testSecret(2))))
		stored := m.destination(id)

		for _, auth := range []string{"api key", "jwt"} {
			if auth == "jwt" {
				// Past the revocation's millisecond: a subscribe started
				// then would lose to it.
				time.Sleep(2 * time.Millisecond)
				m.mustSubscribe(body)
				stored = m.destination(id)
			}
			req := m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions/"+id, nil)
			if auth == "jwt" {
				req = m.withJWT(req, mcpTenant)
			} else {
				req = m.withAPIKey(req)
			}
			resp := m.do(req)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			assert.JSONEq(t, `{"success":true}`, resp.Body.String())
			_, err := m.tenantStore.RetrieveDestination(t.Context(), mcpTenant, id)
			assert.ErrorIs(t, err, tenantstore.ErrDestinationDeleted)

			sent := m.notifier.sent()
			require.NotEmpty(t, sent)
			termination := sent[len(sent)-1]
			assert.Equal(t, mcpTenant, termination.TenantID)
			assert.Equal(t, id, termination.SubscriptionID)
			assert.Equal(t, testCallbackURL, termination.URL)
			assert.Equal(t, stored.CreatedAt.UnixMilli(), termination.CreatedAt.UnixMilli())
			assert.Equal(t, mcpevents.KindForbidden, termination.Error.Kind)
			assert.Equal(t, -32024, termination.Error.Code(), "the configured profile")
			assert.Equal(t, map[string]any{"reason": "access_revoked"}, termination.Error.Data)
			require.NotEmpty(t, termination.Secrets)
			key, err := mcpevents.DecodeSecret(stored.Credentials["secret"])
			require.NoError(t, err)
			assert.Equal(t, key, termination.Secrets[0].Key)
		}
		// The rotation's previous secret signs too, until it expires.
		sent := m.notifier.sent()
		require.Len(t, sent[0].Secrets, 2)
		require.NotNil(t, sent[0].Secrets[1].InvalidAt)
	})

	t.Run("404 unless an mcp subscription of the tenant", func(t *testing.T) {
		m := newMCPTest(t)
		m.putDestination(models.Destination{ID: "des_webhook", Type: "webhook", Topics: models.Topics{"*"}, Config: models.Config{"url": "https://example.com"}})
		for _, id := range []string{"sub_missing", "des_webhook"} {
			resp := m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions/"+id, nil)))
			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "subscription not found")
		}
		assert.NotNil(t, m.destination("des_webhook"))
	})

	t.Run("a full notifier queue still revokes", func(t *testing.T) {
		m := newMCPTest(t)
		m.notifier.full = true
		body := subscribeBody("p1", "order.created")
		m.mustSubscribe(body)
		resp := m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions/"+subscriptionID(t, body), nil)))
		require.Equal(t, http.StatusOK, resp.Code)
		assert.Nil(t, m.destination(subscriptionID(t, body)))
	})
}

func TestMCP_DeleteSubscriptions(t *testing.T) {
	t.Run("principal is required", func(t *testing.T) {
		m := newMCPTest(t)
		for _, query := range []string{"", "?principal="} {
			resp := m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions"+query, nil)))
			requireMCPError(t, resp, "invalid_params", -32602, map[string]any{"field": "principal", "reason": "required"})
		}
	})

	t.Run("revokes every subscription of the principal", func(t *testing.T) {
		m := newMCPTest(t)
		m.mustSubscribe(subscribeBody("alice", "order.created"))
		m.mustSubscribe(subscribeBody("alice", "order.shipped"))
		bob := subscribeBody("bob", "order.created")
		m.mustSubscribe(bob)

		resp := m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions?principal=alice", nil)))
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		assert.JSONEq(t, `{"success":true,"deleted":2}`, resp.Body.String())
		assert.Len(t, m.notifier.sent(), 2)
		assert.Len(t, m.listSubscriptions(""), 1)
		assert.NotNil(t, m.destination(subscriptionID(t, bob)))

		resp = m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions?principal=alice", nil)))
		assert.JSONEq(t, `{"success":true,"deleted":0}`, resp.Body.String())

		require.Eventually(t, func() bool {
			events := m.emitter.byTopic(opevents.TopicTenantSubscriptionUpdated)
			return len(events) > 0 && events[len(events)-1].Data.(opevents.TenantSubscriptionUpdatedData).DestinationsCount == 1
		}, time.Second, 5*time.Millisecond)
	})

	t.Run("a later subscribe is not blocked", func(t *testing.T) {
		m := newMCPTest(t)
		resp := m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions?principal=alice", nil)))
		require.Equal(t, http.StatusOK, resp.Code)
		time.Sleep(2 * time.Millisecond) // the fence is inclusive, in milliseconds
		m.mustSubscribe(subscribeBody("alice", "order.created"))
	})
}
