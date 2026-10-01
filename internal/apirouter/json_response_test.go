package apirouter_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

// panicStore panics when a tenant is retrieved.
type panicStore struct {
	tenantstore.TenantStore
}

func (s *panicStore) RetrieveTenant(context.Context, string) (*models.Tenant, error) {
	panic("test panic")
}

// displayErrorRegistry is a stubRegistry that cannot display destinations.
type displayErrorRegistry struct {
	stubRegistry
}

func (r *displayErrorRegistry) DisplayDestination(*models.Destination) (*destregistry.DestinationDisplay, error) {
	return nil, errors.New("display failed")
}

// Every response is checked for a JSON body by apiTest.do; these cases cover
// the errors raised outside the handlers and assert the envelope.
func TestAPI_ErrorResponsesAreJSON(t *testing.T) {
	publishBody := map[string]any{"tenant_id": "t1", "data": map[string]any{"key": "value"}}

	setup := func(t *testing.T) *apiTest {
		h := newAPITest(t)
		for _, id := range []string{"t1", "t2", "gone"} {
			require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID(id))))
		}
		require.NoError(t, h.tenantStore.DeleteTenant(t.Context(), "gone"))
		return h
	}

	t.Run("401", func(t *testing.T) {
		tests := []struct {
			name string
			req  func(h *apiTest) *http.Request
		}{
			{"no auth header", func(h *apiTest) *http.Request {
				return h.jsonReq(http.MethodGet, "/api/v1/tenants/t1", nil)
			}},
			{"malformed auth header", func(h *apiTest) *http.Request {
				req := h.jsonReq(http.MethodGet, "/api/v1/tenants/t1", nil)
				req.Header.Set("Authorization", "Basic abc")
				return req
			}},
			{"invalid token", func(h *apiTest) *http.Request {
				req := h.jsonReq(http.MethodGet, "/api/v1/tenants/t1", nil)
				req.Header.Set("Authorization", "Bearer not-a-token")
				return req
			}},
			{"jwt without tenant", func(h *apiTest) *http.Request {
				return h.withJWT(h.jsonReq(http.MethodGet, "/api/v1/topics", nil), "")
			}},
			{"jwt of deleted tenant", func(h *apiTest) *http.Request {
				return h.withJWT(h.jsonReq(http.MethodGet, "/api/v1/tenants/gone", nil), "gone")
			}},
			{"jwt of missing tenant", func(h *apiTest) *http.Request {
				return h.withJWT(h.jsonReq(http.MethodGet, "/api/v1/tenants/nope", nil), "nope")
			}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := setup(t)
				testutil.RequireErrorResponse(t, h.do(tt.req(h)), http.StatusUnauthorized, "unauthorized")
			})
		}
	})

	t.Run("403", func(t *testing.T) {
		t.Run("jwt on admin-only route", func(t *testing.T) {
			h := setup(t)
			req := h.withJWT(h.jsonReq(http.MethodPost, "/api/v1/publish", publishBody), "t1")
			testutil.RequireErrorResponse(t, h.do(req), http.StatusForbidden, "forbidden")
		})

		t.Run("jwt on another tenant", func(t *testing.T) {
			h := setup(t)
			req := h.withJWT(h.jsonReq(http.MethodGet, "/api/v1/tenants/t2", nil), "t1")
			testutil.RequireErrorResponse(t, h.do(req), http.StatusForbidden, "forbidden")
		})
	})

	t.Run("404 tenant not found", func(t *testing.T) {
		tests := []struct {
			name string
			path string
		}{
			{"missing tenant", "/api/v1/tenants/nope"},
			{"deleted tenant", "/api/v1/tenants/gone"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := setup(t)
				req := h.withAPIKey(h.jsonReq(http.MethodGet, tt.path, nil))
				testutil.RequireErrorResponse(t, h.do(req), http.StatusNotFound, "tenant not found")
			})
		}

		t.Run("missing tenant without an API key configured", func(t *testing.T) {
			h := newAPITest(t, withAPIKeyConfig(""))
			req := h.jsonReq(http.MethodGet, "/api/v1/tenants/nope", nil)
			testutil.RequireErrorResponse(t, h.do(req), http.StatusNotFound, "tenant not found")
		})
	})

	t.Run("404 outside the routes", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			path   string
			noAuth bool
		}{
			{name: "unknown route", method: http.MethodGet, path: "/api/v1/nope"},
			{name: "unknown route, no auth", method: http.MethodGet, path: "/api/v1/nope", noAuth: true},
			{name: "wrong method", method: http.MethodDelete, path: "/api/v1/publish"},
			{name: "unknown version", method: http.MethodGet, path: "/api/v2/tenants"},
			{name: "api root", method: http.MethodGet, path: "/api"},
			{name: "api root, not GET", method: http.MethodPost, path: "/api"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := setup(t)
				req := h.jsonReq(tt.method, tt.path, nil)
				if !tt.noAuth {
					req = h.withAPIKey(req)
				}
				testutil.RequireErrorResponse(t, h.do(req), http.StatusNotFound, "not found")
			})
		}
	})

	t.Run("422 malformed JSON body", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			path   string
		}{
			{"publish", http.MethodPost, "/api/v1/publish"},
			{"retry", http.MethodPost, "/api/v1/retry"},
			{"upsert tenant", http.MethodPut, "/api/v1/tenants/t1"},
			{"create destination", http.MethodPost, "/api/v1/tenants/t1/destinations"},
			{"update destination", http.MethodPatch, "/api/v1/tenants/t1/destinations/d1"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := setup(t)
				req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("{"))
				req.Header.Set("Content-Type", "application/json")
				testutil.RequireErrorResponse(t, h.do(h.withAPIKey(req)), http.StatusUnprocessableEntity, "invalid JSON")
			})
		}
	})

	t.Run("500 destination cannot be displayed", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			path   string
			body   any
		}{
			{"list destinations", http.MethodGet, "/api/v1/tenants/t1/destinations", nil},
			{"create destination", http.MethodPost, "/api/v1/tenants/t1/destinations", map[string]any{"type": "webhook", "topics": []string{"user.created"}}},
			{"retrieve destination", http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil},
			{"update destination", http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{"topics": []string{"user.created"}}},
			{"disable destination", http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil},
			{"list attempts", http.MethodGet, "/api/v1/attempts?include=destination", nil},
			{"retrieve attempt", http.MethodGet, "/api/v1/attempts/a1?include=destination", nil},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := newAPITest(t, withDestRegistry(&displayErrorRegistry{}))
				require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
				require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1"))))
				e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: e, Attempt: attemptForEvent(e, af.WithID("a1"), af.WithDestinationID("d1"))},
				}))

				req := h.withAPIKey(h.jsonReq(tt.method, tt.path, tt.body))
				testutil.RequireErrorResponse(t, h.do(req), http.StatusInternalServerError, "internal server error")
			})
		}
	})

	t.Run("500 panic in handler chain", func(t *testing.T) {
		h := newAPITest(t, withTenantStore(&panicStore{tenantstore.NewMemTenantStore()}))
		req := h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/tenants/t1", nil))
		testutil.RequireErrorResponse(t, h.do(req), http.StatusInternalServerError, "internal server error")
	})
}
