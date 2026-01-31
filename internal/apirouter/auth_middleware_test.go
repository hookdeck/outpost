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

// mockTenantRetriever implements apirouter.TenantRetriever for unit tests.
type mockTenantRetriever struct {
	tenant *models.Tenant
	err    error
}

func (m *mockTenantRetriever) RetrieveTenant(_ context.Context, _ string) (*models.Tenant, error) {
	return m.tenant, m.err
}

// okHandler is a simple handler that returns 200 when reached.
var okHandler = func(c *gin.Context) {
	c.Status(http.StatusOK)
}

func TestAuthMiddleware(t *testing.T) {
	existingTenant := &models.Tenant{ID: "t1"}
	store := &mockTenantRetriever{tenant: existingTenant}

	t.Run("VPC mode", func(t *testing.T) {
		t.Run("grants admin without auth header", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AuthMiddleware("", testJWTSecret, store, apirouter.AuthOptions{}), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("grants admin ignores auth header", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", apirouter.AuthMiddleware("", testJWTSecret, store, apirouter.AuthOptions{}), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer wrong-key")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run("resolves tenant when RequireTenant", func(t *testing.T) {
			r := gin.New()
			r.GET("/test/:tenant_id", apirouter.AuthMiddleware("", testJWTSecret, store, apirouter.AuthOptions{RequireTenant: true}), okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test/t1", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	})

	t.Run("missing auth header returns 401", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, store, apirouter.AuthOptions{}), okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("malformed bearer prefix returns 401", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, store, apirouter.AuthOptions{}), okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Basic "+testAPIKey)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("empty bearer token returns 401", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, store, apirouter.AuthOptions{}), okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer ")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("valid API key returns 200", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, store, apirouter.AuthOptions{}), okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid token not API key not valid JWT returns 401", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, store, apirouter.AuthOptions{}), okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer not-a-valid-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("valid JWT returns 200", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, store, apirouter.AuthOptions{}), okHandler)

		token, err := apirouter.JWT.New(testJWTSecret, apirouter.JWTClaims{TenantID: "t1"})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("valid JWT on AdminOnly route returns 403", func(t *testing.T) {
		r := gin.New()
		r.GET("/test", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, store, apirouter.AuthOptions{AdminOnly: true}), okHandler)

		token, err := apirouter.JWT.New(testJWTSecret, apirouter.JWTClaims{TenantID: "t1"})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("JWT wrong tenant param returns 403", func(t *testing.T) {
		r := gin.New()
		r.GET("/test/:tenant_id", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, store, apirouter.AuthOptions{}), okHandler)

		token, err := apirouter.JWT.New(testJWTSecret, apirouter.JWTClaims{TenantID: "t1"})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/test/t2", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("JWT deleted tenant returns 401", func(t *testing.T) {
		deletedStore := &mockTenantRetriever{err: tenantstore.ErrTenantDeleted}
		r := gin.New()
		r.GET("/test", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, deletedStore, apirouter.AuthOptions{}), okHandler)

		token, err := apirouter.JWT.New(testJWTSecret, apirouter.JWTClaims{TenantID: "t1"})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("JWT missing tenant returns 401", func(t *testing.T) {
		nilStore := &mockTenantRetriever{tenant: nil}
		r := gin.New()
		r.GET("/test", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, nilStore, apirouter.AuthOptions{}), okHandler)

		token, err := apirouter.JWT.New(testJWTSecret, apirouter.JWTClaims{TenantID: "t1"})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("RequireTenant admin missing tenant returns 404", func(t *testing.T) {
		nilStore := &mockTenantRetriever{tenant: nil}
		r := gin.New()
		r.Use(apirouter.ErrorHandlerMiddleware())
		r.GET("/test/:tenant_id", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, nilStore, apirouter.AuthOptions{RequireTenant: true}), okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test/t1", nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("store error returns 500", func(t *testing.T) {
		errStore := &mockTenantRetriever{err: errors.New("database connection failed")}
		r := gin.New()
		r.Use(apirouter.ErrorHandlerMiddleware())
		r.GET("/test/:tenant_id", apirouter.AuthMiddleware(testAPIKey, testJWTSecret, errStore, apirouter.AuthOptions{RequireTenant: true}), okHandler)

		req := httptest.NewRequest(http.MethodGet, "/test/t1", nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
