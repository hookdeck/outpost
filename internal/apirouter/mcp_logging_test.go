package apirouter_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// retrieveTenantErrorStore fails tenant lookups.
type retrieveTenantErrorStore struct {
	tenantstore.TenantStore
}

func (s *retrieveTenantErrorStore) RetrieveTenant(context.Context, string) (*models.Tenant, error) {
	return nil, errors.New("store unavailable")
}

func TestMCP_Logging(t *testing.T) {
	t.Run("a 5xx subscribe logs its body without the secret", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		m := newMCPTest(t, withMCPAPIOptions(
			withLogger(logging.NewTestLogger(zap.New(core))),
			withTenantStore(&retrieveTenantErrorStore{TenantStore: tenantstore.NewMemTenantStore()}),
		))
		resp := m.subscribe(subscribeBody("p1", "order.created"))
		testutil.RequireErrorResponse(t, resp, http.StatusInternalServerError, "internal server error")

		entries := logs.FilterMessage("request completed").All()
		require.Len(t, entries, 1)
		body, ok := entries[0].ContextMap()["request_body"].(string)
		require.True(t, ok, "the 5xx log has the request body")
		assert.Contains(t, body, `"secret":"[REDACTED]"`)
		assert.Contains(t, body, `"principal":"p1"`)
		assert.NotContains(t, body, "whsec_")
	})

	t.Run("an mcp_error is logged with its kind and data", func(t *testing.T) {
		core, logs := observer.New(zap.InfoLevel)
		m := newMCPTest(t, withMCPAPIOptions(withLogger(logging.NewTestLogger(zap.New(core)))))
		resp := m.subscribe(subscribeBody("p1", "nope"))
		requireMCPError(t, resp, "not_found", -32011, map[string]any{"kind": "event"})

		entries := logs.FilterMessage("request completed").All()
		require.Len(t, entries, 1)
		assert.Equal(t, int64(http.StatusUnprocessableEntity), entries[0].ContextMap()["status"])
		assert.Contains(t, entries[0].ContextMap()["error"], "not_found")
	})
}
