package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/destination"
	"github.com/hookdeck/EventKit/internal/services/api"
	"github.com/hookdeck/EventKit/internal/tenant"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRouterWithAPIKey(t *testing.T) {
	t.Parallel()

	apiKey := "api_key"
	jwtSecret := "jwt_secret"

	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	router := api.NewRouter(
		api.RouterConfig{
			Hostname:  "",
			APIKey:    apiKey,
			JWTSecret: jwtSecret,
		},
		tenant.NewHandlers(logger, redisClient, jwtSecret),
		destination.NewHandlers(redisClient),
	)

	tenantID := "tenantID"
	validToken, err := tenant.JWT.New(jwtSecret, tenantID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("healthcheck should work", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should block unauthenticated request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/"+uuid.New().String(), nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should block tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should allow admin request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("should block unauthenticated request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/tenantID", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should allow admin request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/tenantIDnotfound", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow admin request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/tenantIDnotfound", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/"+tenantID, nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		// A bit awkward that the tenant is not found, but the request is authenticated
		// and the 404 response is handled by the handler which is what we're testing here (routing).
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should block invalid tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/"+tenantID, nil)
		req.Header.Set("Authorization", "Bearer invalid")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestRouterWithoutAPIKey(t *testing.T) {
	t.Parallel()

	apiKey := ""
	jwtSecret := "jwt_secret"

	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)

	router := api.NewRouter(
		api.RouterConfig{
			Hostname:  "",
			APIKey:    apiKey,
			JWTSecret: jwtSecret,
		},
		tenant.NewHandlers(logger, redisClient, jwtSecret),
		destination.NewHandlers(redisClient),
	)

	tenantID := "tenantID"
	validToken, err := tenant.JWT.New(jwtSecret, tenantID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("healthcheck should work", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should allow unauthenticated request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/"+uuid.New().String(), nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("should allow tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("should allow admin request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("should allow unauthenticated request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/tenantID", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow admin request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/tenantIDnotfound", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow admin request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/tenantIDnotfound", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/"+tenantID, nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should block request with invalid bearer authorization header", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/"+tenantID, nil)
		req.Header.Set("Authorization", "NotBearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should block request with bearer authorization header with invalid token", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/"+tenantID, nil)
		req.Header.Set("Authorization", "Bearer invalid")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
