package apirouter_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tenant deleted while a new subscription's callback is being verified
// doesn't get the subscription: the store refuses the create.
func TestMCP_Race_TenantDeletedDuringSubscribe(t *testing.T) {
	m := newMCPTest(t)
	gate := m.gateVerification()
	body := subscribeBody("alice", "order.created")
	done := m.subscribeAsync(body)
	gate.waitEntered(t, 1)

	require.NoError(t, m.tenantStore.DeleteTenant(t.Context(), mcpTenant))

	close(gate.release)
	requireMCPError(t, await(t, done), "not_found", -32011, map[string]any{"kind": "tenant"})
	d, err := m.tenantStore.RetrieveDestination(t.Context(), mcpTenant, subscriptionID(t, body))
	require.NoError(t, err)
	assert.Nil(t, d)
	list, err := m.tenantStore.ListDestination(t.Context(), tenantstore.ListDestinationRequest{TenantID: mcpTenant})
	require.NoError(t, err)
	assert.Empty(t, list)
}
