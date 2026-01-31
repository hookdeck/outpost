package apirouter_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retrieveErrorStore wraps a TenantStore and overrides RetrieveTenant
// to return a configurable error, simulating a store failure.
type retrieveErrorStore struct {
	tenantstore.TenantStore
	err error
}

func (s *retrieveErrorStore) RetrieveTenant(_ context.Context, _ string) (*models.Tenant, error) {
	return nil, s.err
}

func TestAuthMiddleware(t *testing.T) {
	// okHandler is a simple handler that returns 200 when reached.
	okHandler := func(c *gin.Context) {
		c.Status(http.StatusOK)
	}

	t.Run("AdminMiddleware", func(t *testing.T) {
		t.Run("vpc mode grants admin without auth header", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AdminMiddleware(""), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("vpc mode grants admin ignores auth header", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AdminMiddleware(""), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer wrong-key")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("valid api key returns 200", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AdminMiddleware(testAPIKey), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+testAPIKey)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("missing auth header returns 401", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AdminMiddleware(testAPIKey), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("malformed bearer prefix returns 401", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AdminMiddleware(testAPIKey), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Basic "+testAPIKey)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("empty bearer token returns 401", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AdminMiddleware(testAPIKey), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer ")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("wrong api key returns 401", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AdminMiddleware(testAPIKey), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer wrong-key")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	})

	t.Run("AuthenticatedMiddleware", func(t *testing.T) {
		t.Run("vpc mode grants admin without auth header", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AuthenticatedMiddleware("", testJWTSecret), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("valid api key returns 200", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AuthenticatedMiddleware(testAPIKey, testJWTSecret), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+testAPIKey)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("valid jwt returns 200", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AuthenticatedMiddleware(testAPIKey, testJWTSecret), okHandler)

			token, err := apirouter.JWT.New(testJWTSecret, apirouter.JWTClaims{TenantID: "t1"})
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("invalid token neither api key nor jwt returns 401", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AuthenticatedMiddleware(testAPIKey, testJWTSecret), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer not-a-valid-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("missing auth header returns 401", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AuthenticatedMiddleware(testAPIKey, testJWTSecret), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("malformed bearer prefix returns 401", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AuthenticatedMiddleware(testAPIKey, testJWTSecret), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Token "+testAPIKey)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("jwt for wrong tenant param returns 403", func(t *testing.T) {
			r := gin.New()
			r.GET("/test/:tenantID", apirouter.AuthenticatedMiddleware(testAPIKey, testJWTSecret), okHandler)

			token, err := apirouter.JWT.New(testJWTSecret, apirouter.JWTClaims{TenantID: "t1"})
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/test/t2", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	})

	t.Run("resolveTenantMiddleware", func(t *testing.T) {
		t.Run("store error returns 500", func(t *testing.T) {
			store := &retrieveErrorStore{
				TenantStore: tenantstore.NewMemTenantStore(),
				err:         errors.New("database connection failed"),
			}
			h := newAPITest(t, withTenantStore(store))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusInternalServerError, resp.Code)
		})
	})
}
