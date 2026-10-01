package apirouter_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/require"
)

// panicStore panics when a tenant is retrieved.
type panicStore struct {
	tenantstore.TenantStore
}

func (s *panicStore) RetrieveTenant(context.Context, string) (*models.Tenant, error) {
	panic("test panic")
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
				requireErrorResponse(t, h.do(tt.req(h)), http.StatusUnauthorized, "unauthorized")
			})
		}
	})

	t.Run("403", func(t *testing.T) {
		t.Run("jwt on admin-only route", func(t *testing.T) {
			h := setup(t)
			req := h.withJWT(h.jsonReq(http.MethodPost, "/api/v1/publish", publishBody), "t1")
			requireErrorResponse(t, h.do(req), http.StatusForbidden, "forbidden")
		})

		t.Run("jwt on another tenant", func(t *testing.T) {
			h := setup(t)
			req := h.withJWT(h.jsonReq(http.MethodGet, "/api/v1/tenants/t2", nil), "t1")
			requireErrorResponse(t, h.do(req), http.StatusForbidden, "forbidden")
		})
	})

	t.Run("404 outside the routes", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			path   string
		}{
			{"unknown route", http.MethodGet, "/api/v1/nope"},
			{"wrong method", http.MethodDelete, "/api/v1/publish"},
			{"unknown version", http.MethodGet, "/api/v2/tenants"},
			{"api root", http.MethodGet, "/api"},
			{"api root, not GET", http.MethodPost, "/api"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := setup(t)
				req := h.withAPIKey(h.jsonReq(tt.method, tt.path, nil))
				requireErrorResponse(t, h.do(req), http.StatusNotFound, "not found")
			})
		}
	})

	t.Run("500 panic in handler chain", func(t *testing.T) {
		h := newAPITest(t, withTenantStore(&panicStore{tenantstore.NewMemTenantStore()}))
		req := h.withAPIKey(h.jsonReq(http.MethodGet, "/api/v1/tenants/t1", nil))
		requireErrorResponse(t, h.do(req), http.StatusInternalServerError, "internal server error")
	})
}
