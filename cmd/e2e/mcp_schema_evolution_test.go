package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_MCP_BreakingChangeStartup runs Outpost several times against one
// Redis. A breaking change to a topic with a live MCP subscription fails
// startup with the list of changes; TOPICS_ALLOW_BREAKING_CHANGES applies it,
// and the mcp-subscriptions worker ends the subscription with a terminated
// envelope.
func TestE2E_MCP_BreakingChangeStartup(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()

	receiver := newMCPReceiver()
	defer receiver.Close()
	redisConfig := testinfra.NewDragonflyStackConfig(t)

	newConfig := func(orderCreated string, allowBreaking bool) config.Config {
		cfg := configs.Basic(t, configs.BasicOpts{LogStorage: configs.LogStorageTypePostgres, RedisConfig: redisConfig})
		withMCPTopics(&cfg, orderCreated)
		withMCPTestSettings(&cfg)
		cfg.TopicsAllowBreakingChanges = allowBreaking
		return cfg
	}
	// total narrows from number to string: not a widening.
	breaking := strings.Replace(orderCreatedSchema, `"total": {"type": "number"`, `"total": {"type": "string"`, 1)
	require.NotEqual(t, orderCreatedSchema, breaking)

	// 1. A live subscription to order.created.
	first := startStandaloneApp(t, newConfig(orderCreatedSchema, false))
	tenantID := first.createTenant()
	path, callbackURL := receiver.newPath()
	sub := mcpSubscription{Principal: "user_1", Name: "order.created", URL: callbackURL, Secret: newStandardWebhooksSecret()}
	status, body := first.do(http.MethodPut, "/tenants/"+tenantID+"/mcp/subscriptions", sub.body())
	require.Equal(t, http.StatusOK, status, string(body))
	subscriptionID := sub.id(t)
	first.stop()

	// 2. The breaking change fails startup, listing the change.
	t.Run("breaking change refused", func(t *testing.T) {
		cfg := newConfig(breaking, false)
		prepareStandaloneConfig(t, &cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := app.New(&cfg).Run(ctx)
		require.Error(t, err)
		var breakingErr *topicschema.BreakingChangeError
		require.True(t, errors.As(err, &breakingErr), "%v", err)
		assert.Contains(t, err.Error(), "TOPICS_ALLOW_BREAKING_CHANGES")
		assert.Contains(t, err.Error(), "order.created /properties/total")
	})

	// 3. TOPICS_ALLOW_BREAKING_CHANGES applies it and the subscription is
	// ended.
	third := startStandaloneApp(t, newConfig(breaking, true))
	terminated := receiver.waitFor(t, path, 1, isMCPTerminated)[0]
	assert.JSONEq(t, `{"type":"terminated","error":{"code":-32014,"message":"Unsupported","data":{"feature":"payloadSchema","reason":"schema_changed"}}}`, string(terminated.Body))
	assert.Equal(t, subscriptionID, terminated.Header.Get("X-MCP-Subscription-Id"))
	assert.NoError(t, verifyMCPSignature(terminated, sub.Secret))
	status, _ = third.do(http.MethodGet, "/tenants/"+tenantID+"/destinations/"+subscriptionID, nil)
	assert.Equal(t, http.StatusNotFound, status, "the subscription is deleted")

	// Subscribing again works against the new schema (from a later
	// millisecond than the delete: a tie counts as revoked).
	time.Sleep(2 * time.Millisecond)
	status, body = third.do(http.MethodPut, "/tenants/"+tenantID+"/mcp/subscriptions", sub.body())
	require.Equal(t, http.StatusOK, status, string(body))
	third.stop()
	assert.Len(t, receiver.matching(path, isMCPTerminated), 1, "terminated once")

	// 4. The forced configuration is now the applied one: it starts without
	// the flag.
	startStandaloneApp(t, newConfig(breaking, false)).stop()

	// 5. Turning MCP off for order.created (the last step of a topic
	// deprecation) ends the subscriptions that remain, with a terminated
	// envelope saying the event is gone.
	ended := newConfig(breaking, false)
	ended.TopicsSchemas = config.NewTopicSchemas(mcpTopicSchemasJSON(breaking, false))
	fifth := startStandaloneApp(t, ended)
	terminations := receiver.waitFor(t, path, 2, isMCPTerminated)
	assert.JSONEq(t, `{"type":"terminated","error":{"code":-32011,"message":"NotFound","data":{"kind":"event"}}}`, string(terminations[1].Body))
	assert.NoError(t, verifyMCPSignature(terminations[1], sub.Secret))
	status, _ = fifth.do(http.MethodGet, "/tenants/"+tenantID+"/destinations/"+subscriptionID, nil)
	assert.Equal(t, http.StatusNotFound, status)
	status, body = fifth.do(http.MethodPut, "/tenants/"+tenantID+"/mcp/subscriptions", sub.body())
	assert.Equal(t, http.StatusUnprocessableEntity, status, "order.created is no longer an MCP event: %s", body)
}

// TestE2E_MCP_BreakingChangeRefresh checks the refresh path after a forced
// breaking change: a client refreshing a subscription created against the
// old schema, before the mcp-subscriptions worker ends it, gets
// schema_changed instead and the subscription is deleted without a
// terminated envelope. The client's next subscribe creates a new
// subscription against the new schema.
func TestE2E_MCP_BreakingChangeRefresh(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()

	receiver := newMCPReceiver()
	defer receiver.Close()
	redisConfig := testinfra.NewDragonflyStackConfig(t)

	newConfig := func(orderCreated string, allowBreaking bool) config.Config {
		cfg := configs.Basic(t, configs.BasicOpts{LogStorage: configs.LogStorageTypePostgres, RedisConfig: redisConfig})
		withMCPTopics(&cfg, orderCreated)
		withMCPTestSettings(&cfg)
		// No worker pass during the test: the refresh gets there first.
		cfg.MCP.ExpirySweepInterval = config.Duration(time.Hour)
		cfg.TopicsAllowBreakingChanges = allowBreaking
		return cfg
	}
	breaking := strings.Replace(orderCreatedSchema, `"total": {"type": "number"`, `"total": {"type": "string"`, 1)
	require.NotEqual(t, orderCreatedSchema, breaking)

	first := startStandaloneApp(t, newConfig(orderCreatedSchema, false))
	tenantID := first.createTenant()
	path, callbackURL := receiver.newPath()
	sub := mcpSubscription{Principal: "user_1", Name: "order.created", URL: callbackURL, Secret: newStandardWebhooksSecret()}
	status, body := first.do(http.MethodPut, "/tenants/"+tenantID+"/mcp/subscriptions", sub.body())
	require.Equal(t, http.StatusOK, status, string(body))
	subscriptionID := sub.id(t)
	first.stop()

	second := startStandaloneApp(t, newConfig(breaking, true))
	status, body = second.do(http.MethodPut, "/tenants/"+tenantID+"/mcp/subscriptions", sub.body())
	require.Equal(t, http.StatusUnprocessableEntity, status, string(body))
	var mcpErr mcpErrorBody
	require.NoError(t, json.Unmarshal(body, &mcpErr))
	assert.Equal(t, "unsupported", mcpErr.MCPError.Kind)
	assert.Equal(t, -32014, mcpErr.MCPError.Code)
	assert.Equal(t, map[string]any{"feature": "payloadSchema", "reason": "schema_changed"}, mcpErr.MCPError.Data)
	status, _ = second.do(http.MethodGet, "/tenants/"+tenantID+"/destinations/"+subscriptionID, nil)
	assert.Equal(t, http.StatusNotFound, status, "the subscription is deleted")

	// The client re-reads events/list and subscribes again (from a later
	// millisecond than the delete: a tie counts as revoked).
	time.Sleep(2 * time.Millisecond)
	status, body = second.do(http.MethodPut, "/tenants/"+tenantID+"/mcp/subscriptions", sub.body())
	require.Equal(t, http.StatusOK, status, string(body))
	var result mcpSubscribeResult
	require.NoError(t, json.Unmarshal(body, &result))
	assert.Equal(t, subscriptionID, result.ID)
	assert.Nil(t, result.DeliveryStatus, "a new subscription")

	status, body = second.do(http.MethodPut, "/tenants/"+tenantID+"/mcp/subscriptions", sub.body())
	require.Equal(t, http.StatusOK, status, string(body))
	require.NoError(t, json.Unmarshal(body, &result))
	assert.NotNil(t, result.DeliveryStatus, "which refreshes normally")

	time.Sleep(500 * time.Millisecond)
	assert.Empty(t, receiver.matching(path, isMCPTerminated), "no terminated envelope")
	second.stop()
}
