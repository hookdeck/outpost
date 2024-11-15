package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/eventtracer"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/services/api"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

const baseAPIPath = "/api/v1"

func testRouterWithCHDB(t *testing.T, config *clickhouse.ClickHouseConfig) clickhouse.DB {
	chDB, err := clickhouse.New(config)
	require.NoError(t, err)
	return chDB
}

func setupTestRouter(t *testing.T, apiKey, jwtSecret string, funcs ...func(t *testing.T) clickhouse.DB) (http.Handler, *otelzap.Logger, *redis.Client) {
	gin.SetMode(gin.TestMode)
	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	deliveryMQ := deliverymq.New()
	deliveryMQ.Init(context.Background())
	eventTracer := eventtracer.NewNoopEventTracer()
	entityStore := setupTestEntityStore(t, redisClient, nil)
	logStore := setupTestLogStore(t, funcs...)
	eventHandler := publishmq.NewEventHandler(logger, redisClient, deliveryMQ, entityStore, eventTracer, testutil.TestTopics)
	router := api.NewRouter(
		api.RouterConfig{
			Hostname:  "",
			APIKey:    apiKey,
			JWTSecret: jwtSecret,
			Topics:    testutil.TestTopics,
		},
		logger,
		redisClient,
		deliveryMQ,
		entityStore,
		logStore,
		eventHandler,
	)
	return router, logger, redisClient
}

func setupTestLogStore(t *testing.T, funcs ...func(t *testing.T) clickhouse.DB) models.LogStore {
	var chDB clickhouse.DB
	for _, f := range funcs {
		chDB = f(t)
	}
	return models.NewLogStore(chDB)
}

func setupTestEntityStore(_ *testing.T, redisClient *redis.Client, cipher models.Cipher) models.EntityStore {
	if cipher == nil {
		cipher = models.NewAESCipher("secret")
	}
	return models.NewEntityStore(redisClient, cipher)
}

func TestRouterWithAPIKey(t *testing.T) {
	t.Parallel()

	apiKey := "api_key"
	jwtSecret := "jwt_secret"
	router, _, _ := setupTestRouter(t, apiKey, jwtSecret)

	tenantID := "tenantID"
	validToken, err := api.JWT.New(jwtSecret, tenantID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("healthcheck should work", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/healthz", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should block unauthenticated request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+uuid.New().String(), nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should block tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should allow admin request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+uuid.New().String(), nil)
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
	validToken, err := api.JWT.New(jwtSecret, tenantID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("healthcheck should work", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/healthz", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should allow unauthenticated request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+uuid.New().String(), nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("should allow tenant-auth request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("should allow admin request to admin routes", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
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

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should block request with invalid bearer authorization header", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID, nil)
		req.Header.Set("Authorization", "NotBearer "+validToken)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should block request with bearer authorization header with invalid token", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID, nil)
		req.Header.Set("Authorization", "Bearer invalid")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
