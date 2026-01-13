package apirouter_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/telemetry"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const baseAPIPath = "/api/v1"

func setupTestRouter(t *testing.T, apiKey, jwtSecret string, funcs ...func(t *testing.T) clickhouse.DB) (http.Handler, *logging.Logger, redis.Client) {
	gin.SetMode(gin.TestMode)
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	deliveryMQ := deliverymq.New()
	deliveryMQ.Init(context.Background())
	eventTracer := eventtracer.NewNoopEventTracer()
	entityStore := setupTestEntityStore(t, redisClient, nil)
	logStore := setupTestLogStore(t, funcs...)
	eventHandler := publishmq.NewEventHandler(logger, deliveryMQ, entityStore, eventTracer, testutil.TestTopics, idempotence.New(redisClient, idempotence.WithSuccessfulTTL(24*time.Hour)))
	router := apirouter.NewRouter(
		apirouter.RouterConfig{
			ServiceName: "",
			APIKey:      apiKey,
			JWTSecret:   jwtSecret,
			Topics:      testutil.TestTopics,
		},
		logger,
		redisClient,
		deliveryMQ,
		entityStore,
		logStore,
		eventHandler,
		&telemetry.NoopTelemetry{},
	)
	return router, logger, redisClient
}

func setupTestLogStore(t *testing.T, funcs ...func(t *testing.T) clickhouse.DB) logstore.LogStore {
	var chDB clickhouse.DB
	for _, f := range funcs {
		chDB = f(t)
	}
	if chDB == nil {
		return logstore.NewNoopLogStore()
	}
	logStore, err := logstore.NewLogStore(context.Background(), logstore.DriverOpts{
		CH: chDB,
	})
	require.NoError(t, err)
	return logStore
}

func setupTestEntityStore(_ *testing.T, redisClient redis.Client, cipher models.Cipher) models.EntityStore {
	if cipher == nil {
		cipher = models.NewAESCipher("secret")
	}
	return models.NewEntityStore(redisClient,
		models.WithCipher(cipher),
		models.WithAvailableTopics(testutil.TestTopics),
	)
}

func TestRouterWithAPIKey(t *testing.T) {
	t.Parallel()

	apiKey := "api_key"
	jwtSecret := "jwt_secret"
	router, _, _ := setupTestRouter(t, apiKey, jwtSecret)

	tenantID := "tenantID"
	validToken, err := apirouter.JWT.New(jwtSecret, tenantID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("should block unauthenticated request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+idgen.String(), nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should block tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+idgen.String(), nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should allow admin request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+idgen.String(), nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("should block unauthenticated request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenantID", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should allow admin request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenantIDnotfound", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow admin request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenantIDnotfound", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID, nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		// A bit awkward that the tenant is not found, but the request is authenticated
		// and the 404 response is handled by the handler which is what we're testing here (routing).
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should block invalid tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID, nil)
		req.Header.Set("Authorization", "Bearer invalid")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestRouterWithoutAPIKey(t *testing.T) {
	t.Parallel()

	apiKey := ""
	jwtSecret := "jwt_secret"

	router, _, _ := setupTestRouter(t, apiKey, jwtSecret)

	tenantID := "tenantID"
	validToken, err := apirouter.JWT.New(jwtSecret, tenantID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("should allow unauthenticated request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+idgen.String(), nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("should allow tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+idgen.String(), nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("should allow admin request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+idgen.String(), nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("should return 404 for JWT-only routes when apiKey is empty", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/destinations", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for JWT-only routes with invalid token when apiKey is empty", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/destinations", nil)
		req.Header.Set("Authorization", "Bearer invalid")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for JWT-only routes with invalid bearer format when apiKey is empty", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/destinations", nil)
		req.Header.Set("Authorization", "NotBearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow unauthenticated request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenantID", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow admin request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenantIDnotfound", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow tenant-auth request to tenant routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID, nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestTokenAndPortalRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		apiKey    string
		jwtSecret string
		path      string
	}{
		{
			name:      "token route should return 404 when apiKey is empty",
			apiKey:    "",
			jwtSecret: "secret",
			path:      "/tenant-id/token",
		},
		{
			name:      "token route should return 404 when jwtSecret is empty",
			apiKey:    "key",
			jwtSecret: "",
			path:      "/tenant-id/token",
		},
		{
			name:      "portal route should return 404 when apiKey is empty",
			apiKey:    "",
			jwtSecret: "secret",
			path:      "/tenant-id/portal",
		},
		{
			name:      "portal route should return 404 when jwtSecret is empty",
			apiKey:    "key",
			jwtSecret: "",
			path:      "/tenant-id/portal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _, _ := setupTestRouter(t, tt.apiKey, tt.jwtSecret)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", baseAPIPath+"/"+tt.path, nil)
			if tt.apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+tt.apiKey)
			}
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func TestTenantsRoutePrefix(t *testing.T) {
	t.Parallel()

	apiKey := "api_key"
	router, _, _ := setupTestRouter(t, apiKey, "jwt_secret")

	t.Run("new /tenants/ path should work without deprecation headers", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/tenants/"+idgen.String(), nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Empty(t, w.Header().Get("Deprecation"), "new path should not have Deprecation header")
		assert.Empty(t, w.Header().Get("Link"), "new path should not have Link header")
	})

	t.Run("old path should work with deprecation headers", func(t *testing.T) {
		t.Parallel()

		tenantID := idgen.String()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+tenantID, nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, "true", w.Header().Get("Deprecation"), "old path should have Deprecation header")
		assert.Contains(t, w.Header().Get("Link"), "/tenants/"+tenantID, "Link header should point to new path")
		assert.Contains(t, w.Header().Get("Link"), "rel=\"successor-version\"", "Link header should have successor-version rel")
	})

	t.Run("both paths should return identical response for tenant GET", func(t *testing.T) {
		t.Parallel()

		// First create a tenant
		tenantID := idgen.String()
		createReq, _ := http.NewRequest("PUT", baseAPIPath+"/tenants/"+tenantID, nil)
		createReq.Header.Set("Authorization", "Bearer "+apiKey)
		createW := httptest.NewRecorder()
		router.ServeHTTP(createW, createReq)
		require.Equal(t, http.StatusCreated, createW.Code)

		// GET via new path
		newPathW := httptest.NewRecorder()
		newPathReq, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID, nil)
		newPathReq.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(newPathW, newPathReq)

		// GET via old path
		oldPathW := httptest.NewRecorder()
		oldPathReq, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID, nil)
		oldPathReq.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(oldPathW, oldPathReq)

		// Both should return same status and body
		assert.Equal(t, http.StatusOK, newPathW.Code)
		assert.Equal(t, http.StatusOK, oldPathW.Code)
		assert.Equal(t, newPathW.Body.String(), oldPathW.Body.String(), "response bodies should be identical")

		// Only old path should have deprecation headers
		assert.Empty(t, newPathW.Header().Get("Deprecation"))
		assert.Equal(t, "true", oldPathW.Header().Get("Deprecation"))
	})

	t.Run("non-tenant routes should not have deprecation headers", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/publish", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		// Even if request fails validation, we check headers are not set
		assert.Empty(t, w.Header().Get("Deprecation"), "/publish should not have deprecation headers")
	})
}
