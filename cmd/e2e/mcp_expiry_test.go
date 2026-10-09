package e2e_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	opeventsmock "github.com/hookdeck/outpost/cmd/e2e/opevents"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mcpSweepGrace is the mcp-subscriptions worker's grace after expires_at
// before it deletes a subscription (mcpworker.DefaultGrace), plus a margin
// for the sweep interval.
const mcpSweepGrace = 60*time.Second + 15*time.Second

// TestE2E_MCP_ExpirySweep checks that the mcp-subscriptions worker deletes
// an expired subscription in the background, without any call from the
// client, and reports it once with mcp.subscription.expired. The worker
// waits a fixed 60s grace past expires_at, so this test takes about a
// minute; it runs in parallel with the others.
func TestE2E_MCP_ExpirySweep(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()

	receiver := newMCPReceiver()
	defer receiver.Close()
	oeServer := opeventsmock.NewMockServer()
	require.NoError(t, oeServer.Start())
	defer func() { _ = oeServer.Stop() }()

	cfg := configs.Basic(t, configs.BasicOpts{
		LogStorage:  configs.LogStorageTypePostgres,
		RedisConfig: testinfra.NewDragonflyStackConfig(t),
	})
	withMCPTopics(&cfg, orderCreatedSchema)
	withMCPTestSettings(&cfg)
	cfg.OperatorEvents.Topics = []string{"*"}
	cfg.OperatorEvents.HTTP.URL = oeServer.GetURL()
	cfg.OperatorEvents.HTTP.SigningSecret = "test-opevents-secret"
	a := startStandaloneApp(t, cfg)

	tenantID := a.createTenant()
	path, callbackURL := receiver.newPath()
	sub := mcpSubscription{Principal: "user_1", Name: "order.created", URL: callbackURL, Secret: newStandardWebhooksSecret(), TTLMs: 1000}
	status, body := a.do(http.MethodPut, "/tenants/"+tenantID+"/mcp/subscriptions", sub.body())
	require.Equal(t, http.StatusOK, status, string(body))
	id := sub.id(t)
	status, body = a.do(http.MethodGet, "/tenants/"+tenantID+"/destinations/"+id, nil)
	require.Equal(t, http.StatusOK, status, string(body))
	var dest struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(body, &dest))

	// The subscribe reports the new subscription; start counting after it.
	waitForTenantUpdate := func(count int) map[string]any {
		t.Helper()
		deadline := time.Now().Add(alertPollTimeout)
		for time.Now().Before(deadline) {
			if events := oeServer.GetEventsByTopic("tenant.subscription.updated"); len(events) > 0 {
				var data map[string]any
				require.NoError(t, json.Unmarshal(events[len(events)-1].Event.Data, &data))
				if data["destinations_count"] == float64(count) {
					return data
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("no tenant.subscription.updated with destinations_count %d", count)
		return nil
	}
	waitForTenantUpdate(1)
	oeServer.Reset()

	// Poll for the background deletion.
	var expired []opeventsmock.ReceivedEvent
	deadline := time.Now().Add(time.Until(dest.ExpiresAt) + mcpSweepGrace)
	for time.Now().Before(deadline) {
		if expired = oeServer.GetEventsByTopic("mcp.subscription.expired"); len(expired) > 0 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	require.Len(t, expired, 1, "the sweep reports the expiry")
	assert.GreaterOrEqual(t, time.Since(dest.ExpiresAt), 60*time.Second, "after the grace")

	var data struct {
		TenantID       string    `json:"tenant_id"`
		SubscriptionID string    `json:"subscription_id"`
		Principal      string    `json:"principal"`
		Topic          string    `json:"topic"`
		URL            string    `json:"url"`
		ExpiresAt      time.Time `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(expired[0].Event.Data, &data))
	assert.Equal(t, tenantID, data.TenantID)
	assert.Equal(t, id, data.SubscriptionID)
	assert.Equal(t, sub.Principal, data.Principal)
	assert.Equal(t, "order.created", data.Topic)
	assert.Equal(t, callbackURL, data.URL)
	assert.True(t, dest.ExpiresAt.Equal(data.ExpiresAt))

	status, _ = a.do(http.MethodGet, "/tenants/"+tenantID+"/destinations/"+id, nil)
	assert.Equal(t, http.StatusNotFound, status, "deleted")
	var list []json.RawMessage
	status, body = a.do(http.MethodGet, "/tenants/"+tenantID+"/mcp/subscriptions", nil)
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal(body, &list))
	assert.Empty(t, list)

	// The tenant's subscription count changed.
	update := waitForTenantUpdate(0)
	assert.Equal(t, float64(1), update["previous_destinations_count"])

	// A few more passes: reported once, no terminated envelope (expiry
	// sends none).
	time.Sleep(3 * time.Second)
	assert.Len(t, oeServer.GetEventsByTopic("mcp.subscription.expired"), 1)
	assert.Empty(t, receiver.matching(path, isMCPTerminated))
}
