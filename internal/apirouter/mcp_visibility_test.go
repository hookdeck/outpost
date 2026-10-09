package apirouter_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// visibilityTest holds a webhook destination "des_webhook" and an mcp
// subscription in mcpTenant, each with one attempt.
type visibilityTest struct {
	*mcpTest
	subID          string
	webhookAttempt string
	mcpAttempt     string
}

func newVisibilityTest(t *testing.T) *visibilityTest {
	t.Helper()
	m := newMCPTest(t)
	body := subscribeBody("p1", "order.created")
	m.mustSubscribe(body)
	v := &visibilityTest{mcpTest: m, subID: subscriptionID(t, body), webhookAttempt: "att_webhook", mcpAttempt: "att_mcp"}
	m.putDestination(models.Destination{ID: "des_webhook", Type: "webhook", Topics: models.Topics{"*"}, Config: models.Config{"url": "https://example.com"}})

	e1 := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID(mcpTenant), ef.WithTopic("order.created"), ef.WithMatchedDestinationIDs([]string{"des_webhook"}))
	require.NoError(t, m.logStore.InsertMany(t.Context(), []*models.LogEntry{
		{Event: e1, Attempt: attemptForEvent(e1, af.WithID(v.webhookAttempt), af.WithDestinationID("des_webhook"), af.WithDestinationType("webhook"), af.WithTime(time.Now().Add(-time.Second)))},
		{Event: e1, Attempt: attemptForEvent(e1, af.WithID(v.mcpAttempt), af.WithDestinationID(v.subID), af.WithDestinationType(models.DestinationTypeMCP))},
	}))
	return v
}

func (v *visibilityTest) get(version, path string) *jsonResponse {
	v.t.Helper()
	resp := v.do(v.withAPIKey(v.jsonReq(http.MethodGet, "/api/"+version+path, nil)))
	return &jsonResponse{code: resp.Code, body: resp.Body.Bytes()}
}

type jsonResponse struct {
	code int
	body []byte
}

func (r *jsonResponse) ids(t *testing.T) []string {
	t.Helper()
	require.Equal(t, http.StatusOK, r.code, string(r.body))
	var list []struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(r.body, &list))
	var ids []string
	for _, d := range list {
		ids = append(ids, d.ID)
	}
	return ids
}

func (r *jsonResponse) attemptIDs(t *testing.T) []string {
	t.Helper()
	require.Equal(t, http.StatusOK, r.code, string(r.body))
	var page struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	require.NoError(t, json.Unmarshal(r.body, &page))
	ids := []string{}
	for _, a := range page.Models {
		ids = append(ids, a.ID)
	}
	return ids
}

func TestMCPVisibility_Destinations(t *testing.T) {
	v := newVisibilityTest(t)
	base := "/tenants/" + mcpTenant + "/destinations"

	t.Run("v1 lists no mcp destination, v2 lists them", func(t *testing.T) {
		assert.Equal(t, []string{"des_webhook"}, v.get("v1", base).ids(t))
		assert.Empty(t, v.get("v1", base+"?type=mcp").ids(t))
		assert.ElementsMatch(t, []string{"des_webhook", v.subID}, v.get("v2", base).ids(t))
	})

	t.Run("v1 retrieves no mcp destination", func(t *testing.T) {
		resp := v.do(v.withAPIKey(v.jsonReq(http.MethodGet, "/api/v1"+base+"/"+v.subID, nil)))
		testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "destination not found")

		resp = v.do(v.withJWT(v.jsonReq(http.MethodGet, "/api/v2"+base+"/"+v.subID, nil), mcpTenant))
		require.Equal(t, http.StatusOK, resp.Code)
		var d map[string]any
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &d))
		assert.Equal(t, "mcp", d["type"])
		assert.NotEmpty(t, d["expires_at"])
		assert.NotEqual(t, testSecret(1), d["credentials"].(map[string]any)["secret"], "secrets are masked")
	})

	t.Run("POST type mcp is refused in both versions", func(t *testing.T) {
		for _, version := range []string{"v1", "v2"} {
			resp := v.do(v.withAPIKey(v.jsonReq(http.MethodPost, "/api/"+version+base, map[string]any{
				"type":   "mcp",
				"topics": []string{"order.created"},
				"config": map[string]string{"url": testCallbackURL},
			})))
			testutil.RequireErrorResponse(t, resp, http.StatusBadRequest,
				"mcp destinations can't be created here: MCP clients subscribe through PUT /tenants/{tenant_id}/mcp/subscriptions")
		}
	})

	t.Run("PATCH, DELETE and disable are refused in v2, 404 in v1", func(t *testing.T) {
		cases := []struct {
			method, path, message string
			body                  any
		}{
			{http.MethodPatch, "", "mcp destinations can't be updated: MCP clients change them by subscribing again", map[string]any{"metadata": map[string]string{"a": "b"}}},
			{http.MethodDelete, "", "mcp destinations can't be deleted here: use DELETE /tenants/{tenant_id}/mcp/subscriptions/{subscription_id}", nil},
			{http.MethodPut, "/disable", "mcp destinations can't be disabled: end them with DELETE /tenants/{tenant_id}/mcp/subscriptions/{subscription_id}", nil},
		}
		for _, c := range cases {
			path := base + "/" + v.subID + c.path
			resp := v.do(v.withJWT(v.jsonReq(c.method, "/api/v2"+path, c.body), mcpTenant))
			testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, c.message)
			resp = v.do(v.withAPIKey(v.jsonReq(c.method, "/api/v1"+path, c.body)))
			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "destination not found")
		}
		d := v.destination(v.subID)
		require.NotNil(t, d)
		assert.Nil(t, d.DisabledAt)
		assert.Nil(t, d.Metadata)
	})
}

func TestMCPVisibility_Enable(t *testing.T) {
	v := newVisibilityTest(t)
	path := "/tenants/" + mcpTenant + "/destinations/" + v.subID + "/enable"

	t.Run("an enabled subscription is left alone", func(t *testing.T) {
		resp := v.do(v.withJWT(v.jsonReq(http.MethodPut, "/api/v2"+path, nil), mcpTenant))
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		resets, _ := v.alerts.calls()
		assert.Empty(t, resets)
	})

	t.Run("v1 doesn't see it", func(t *testing.T) {
		resp := v.do(v.withAPIKey(v.jsonReq(http.MethodPut, "/api/v1"+path, nil)))
		testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "destination not found")
	})

	t.Run("re-enables like a refresh", func(t *testing.T) {
		ctx := t.Context()
		before := v.destination(v.subID)
		_, err := v.tenantStore.DisableDestination(ctx, mcpTenant, v.subID, time.Now())
		require.NoError(t, err)
		_, err = v.tenantStore.ParkRetry(ctx, mcpTenant, v.subID, "task-1", 1000, time.Now().Add(time.Hour))
		require.NoError(t, err)

		resp := v.do(v.withJWT(v.jsonReq(http.MethodPut, "/api/v2"+path, nil), mcpTenant))
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
		assert.Nil(t, body["disabled_at"])

		after := v.destination(v.subID)
		assert.Nil(t, after.DisabledAt)
		assert.Equal(t, before.Credentials, after.Credentials, "nothing else changes")
		assert.Equal(t, before.ExpiresAt.UnixMilli(), after.ExpiresAt.UnixMilli())
		assert.Equal(t, before.CreatedAt.UnixMilli(), after.CreatedAt.UnixMilli())

		resets, disabled := v.alerts.calls()
		assert.Equal(t, []string{v.subID}, resets)
		assert.Equal(t, []bool{true}, disabled, "reset before re-enabling")
		resumed := v.resumer.resumed()
		require.Len(t, resumed, 1)
		members, err := v.tenantStore.PopResumeMembers(ctx, mcpTenant, resumed[0].key, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{"task-1"}, members)
	})
}

func TestMCPVisibility_Attempts(t *testing.T) {
	v := newVisibilityTest(t)
	list := "/attempts?tenant_id=" + mcpTenant

	t.Run("v1 lists no attempt to an mcp destination", func(t *testing.T) {
		assert.Equal(t, []string{v.webhookAttempt}, v.get("v1", list).attemptIDs(t))
		assert.Empty(t, v.get("v1", list+"&destination_type=mcp").attemptIDs(t))
		assert.Equal(t, []string{v.webhookAttempt}, v.get("v1", list+"&destination_type[0]=mcp&destination_type[1]=webhook").attemptIDs(t))
		assert.Equal(t, []string{v.webhookAttempt}, v.get("v1", list+"&destination_type=webhook").attemptIDs(t))

		resp := v.do(v.withJWT(v.jsonReq(http.MethodGet, "/api/v1/attempts", nil), mcpTenant))
		require.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, []string{v.webhookAttempt}, (&jsonResponse{code: resp.Code, body: resp.Body.Bytes()}).attemptIDs(t))
	})

	t.Run("v2 lists every attempt", func(t *testing.T) {
		assert.Equal(t, []string{v.mcpAttempt, v.webhookAttempt}, v.get("v2", list).attemptIDs(t))
		assert.Equal(t, []string{v.mcpAttempt}, v.get("v2", list+"&destination_type=mcp").attemptIDs(t))
		assert.Equal(t, []string{v.mcpAttempt}, v.get("v2", "/tenants/"+mcpTenant+"/destinations/"+v.subID+"/attempts").attemptIDs(t))
	})

	t.Run("v1 retrieves no attempt to an mcp destination", func(t *testing.T) {
		for _, path := range []string{
			"/attempts/" + v.mcpAttempt + "?tenant_id=" + mcpTenant,
			"/tenants/" + mcpTenant + "/destinations/" + v.subID + "/attempts/" + v.mcpAttempt,
		} {
			resp := v.get("v1", path)
			assert.Equal(t, http.StatusNotFound, resp.code, path)
			assert.Equal(t, http.StatusOK, v.get("v2", path).code, path)
		}
		resp := v.get("v1", "/tenants/"+mcpTenant+"/destinations/"+v.subID+"/attempts")
		assert.Equal(t, http.StatusNotFound, resp.code)
		assert.Equal(t, http.StatusOK, v.get("v1", "/attempts/"+v.webhookAttempt+"?tenant_id="+mcpTenant).code)
	})
}

func TestMCPVisibility_Publish(t *testing.T) {
	v := newVisibilityTest(t)
	v.eventHandler.result = &publishmq.HandleResult{
		EventID:                 "e1",
		DestinationIDs:          []string{"des_webhook", v.subID},
		MatchedDestinationTypes: []string{"webhook", models.DestinationTypeMCP},
	}
	publish := map[string]any{"tenant_id": mcpTenant, "topic": "order.created", "data": map[string]any{"total": 1}}

	resp := v.do(v.withAPIKey(v.jsonReq(http.MethodPost, "/api/v1/publish", publish)))
	require.Equal(t, http.StatusAccepted, resp.Code)
	assert.JSONEq(t, `{"id":"e1","duplicate":false,"destination_ids":["des_webhook"]}`, resp.Body.String())

	resp = v.do(v.withAPIKey(v.jsonReq(http.MethodPost, "/api/v2/publish", publish)))
	require.Equal(t, http.StatusAccepted, resp.Code)
	assert.JSONEq(t, `{"id":"e1","duplicate":false,"destination_ids":["des_webhook","`+v.subID+`"]}`, resp.Body.String())
	assert.Len(t, v.eventHandler.result.DestinationIDs, 2, "the handler's result isn't modified")

	// Without types, nothing is hidden.
	v.eventHandler.result = &publishmq.HandleResult{EventID: "e2", DestinationIDs: []string{"a", "b"}}
	resp = v.do(v.withAPIKey(v.jsonReq(http.MethodPost, "/api/v1/publish", publish)))
	assert.JSONEq(t, `{"id":"e2","duplicate":false,"destination_ids":["a","b"]}`, resp.Body.String())
}

func TestMCPVisibility_RetryExpired(t *testing.T) {
	v := newVisibilityTest(t)
	d := v.destination(v.subID)
	past := time.Now().Add(-time.Second)
	d.ExpiresAt = &past
	v.putDestination(*d)

	for _, version := range []string{"v1", "v2"} {
		resp := v.do(v.withAPIKey(v.jsonReq(http.MethodPost, "/api/"+version+"/retry", map[string]any{
			"event_id":       "e1",
			"destination_id": v.subID,
		})))
		testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, "Destination has expired")
		assert.JSONEq(t, `{"error":"destination_expired"}`, string(rawField(t, resp.Body.Bytes(), "data")))
	}
	assert.Empty(t, v.deliveryPub.calls)
}
