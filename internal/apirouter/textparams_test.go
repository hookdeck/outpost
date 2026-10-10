package apirouter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

// A NUL byte or invalid UTF-8 in a path or query parameter is answered with a
// 400 naming the parameter, before it reaches a store: PostgreSQL refuses such
// a text parameter and the request used to fail with a 500.
func TestAPI_InvalidTextParamReturns400(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		message string
	}{
		{"NUL in a query parameter", "/api/v1/events?tenant_id=%00", "invalid tenant_id: must be valid UTF-8 without NUL bytes"},
		{"NUL inside a query value", "/api/v1/attempts?event_id=e1%00", "invalid event_id: must be valid UTF-8 without NUL bytes"},
		{"invalid UTF-8 in a query parameter", "/api/v1/events?topic=%FF", "invalid topic: must be valid UTF-8 without NUL bytes"},
		{"one bad value of an array parameter", "/api/v1/events?topic[]=user.created&topic[]=%C3", "invalid topic[]: must be valid UTF-8 without NUL bytes"},
		{"NUL in a path parameter", "/api/v1/events/%00", "invalid event_id: must be valid UTF-8 without NUL bytes"},
		{"invalid UTF-8 in a path parameter", "/api/v1/attempts/%FF%FE", "invalid attempt_id: must be valid UTF-8 without NUL bytes"},
		{"NUL in a nested path parameter", "/api/v1/tenants/t1/destinations/d1/attempts/a%001", "invalid attempt_id: must be valid UTF-8 without NUL bytes"},
		{"NUL in a metrics filter", "/api/v1/metrics/events?tenant_id=%00", "invalid tenant_id: must be valid UTF-8 without NUL bytes"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newAPITest(t)
			require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
			require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1"))))

			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			resp := h.do(h.withAPIKey(req))

			testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, tc.message)
		})
	}
}

// Text that is valid UTF-8 is not touched, including non-ASCII.
func TestAPI_ValidTextParamIsAccepted(t *testing.T) {
	for _, target := range []string{
		"/api/v1/events?tenant_id=t1",
		"/api/v1/events?topic=caf%C3%A9",
		"/api/v1/attempts?event_id=%F0%9F%8E%89",
	} {
		t.Run(target, func(t *testing.T) {
			h := newAPITest(t)
			require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))

			req := httptest.NewRequest(http.MethodGet, target, nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code, "body: %s", resp.Body.String())
		})
	}
}
