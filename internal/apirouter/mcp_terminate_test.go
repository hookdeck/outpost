package apirouter_test

import (
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// With MCP_SEND_TERMINATED=false, revoking sends nothing and logs no
// warning: skipping the envelope is what the operator asked for.
func TestMCP_RevokeWithTerminatedDisabled(t *testing.T) {
	notifier, err := mcpevents.NewNotifier(mcpevents.NotifierConfig{Disabled: true})
	require.NoError(t, err)
	core, logs := observer.New(zap.InfoLevel)
	m := newMCPTest(t,
		withMCPAPIOptions(withLogger(logging.NewTestLogger(zap.New(core)))),
		withMCPDeps(func(d *apirouter.MCPDeps) { d.Notifier = notifier }),
	)

	one := subscribeBody("p1", "order.created")
	m.mustSubscribe(one)
	for _, total := range []int{200, 300} {
		m.mustSubscribe(subscribeBody("p1", "order.created", withArguments(map[string]any{"total": map[string]any{"$gte": total}})))
	}

	resp := m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions/"+subscriptionID(t, one), nil)))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	resp = m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions?principal=p1", nil)))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.JSONEq(t, `{"success":true,"deleted":2}`, resp.Body.String())

	assert.Zero(t, logs.FilterMessage("mcp terminated envelope not queued").Len())
	assert.Zero(t, logs.FilterLevelExact(zap.WarnLevel).Len(), "no warning at all")
}

// Control: an envelope a full queue drops is still logged.
func TestMCP_RevokeWithFullNotifierWarns(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	m := newMCPTest(t, withMCPAPIOptions(withLogger(logging.NewTestLogger(zap.New(core)))))
	m.notifier.full = true
	body := subscribeBody("p1", "order.created")
	m.mustSubscribe(body)
	resp := m.do(m.withAPIKey(m.jsonReq(http.MethodDelete, "/api/v2/tenants/"+mcpTenant+"/mcp/subscriptions/"+subscriptionID(t, body), nil)))
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, 1, logs.FilterMessage("mcp terminated envelope not queued").Len())
}
