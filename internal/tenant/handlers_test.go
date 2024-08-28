package tenant_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/tenant"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func setupRouter(tenantHandlers *tenant.TenantHandlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.PUT("/:tenantID", tenantHandlers.Upsert)
	r.GET("/:tenantID", tenantHandlers.Retrieve)
	r.DELETE("/:tenantID", tenantHandlers.Delete)
	r.GET("/:tenantID/portal", tenantHandlers.RetrievePortal)
	return r
}

func TestDestinationUpsertHandler(t *testing.T) {
	t.Parallel()

	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	model := tenant.NewTenantModel(redisClient)
	handlers := tenant.NewHandlers(logger, redisClient, "")
	router := setupRouter(handlers)

	t.Run("should create when there's no existing tenant", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()

		id := uuid.New().String()
		req, _ := http.NewRequest("PUT", "/"+id, nil)
		router.ServeHTTP(w, req)

		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, id, response["id"])
		assert.NotEqual(t, "", response["created_at"])
	})

	t.Run("should return tenant when there's already one", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := tenant.Tenant{
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
		}
		model.Set(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/"+existingResource.ID, nil)
		router.ServeHTTP(w, req)
		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		// Test
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, existingResource.ID, response["id"])
		createdAt, err := time.Parse(time.RFC3339Nano, response["created_at"].(string))
		if err != nil {
			t.Fatal(err)
		}
		assert.True(t, existingResource.CreatedAt.Equal(createdAt))

		// Cleanup
		model.Clear(context.Background(), existingResource.ID)
	})
}

func TestTenantRetrieveHandler(t *testing.T) {
	t.Parallel()

	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	model := tenant.NewTenantModel(redisClient)
	handlers := tenant.NewHandlers(logger, redisClient, "")
	router := setupRouter(handlers)

	t.Run("should return 404 when there's no tenant", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should retrieve tenant", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := tenant.Tenant{
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
		}
		model.Set(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/"+existingResource.ID, nil)
		router.ServeHTTP(w, req)
		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		// Test
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, existingResource.ID, response["id"])
		createdAt, err := time.Parse(time.RFC3339Nano, response["created_at"].(string))
		if err != nil {
			t.Fatal(err)
		}
		assert.True(t, existingResource.CreatedAt.Equal(createdAt))

		// Cleanup
		model.Clear(context.Background(), existingResource.ID)
	})
}

func TestTenantDeleteHandler(t *testing.T) {
	t.Parallel()

	logger := testutil.CreateTestLogger(t)
	redisClient := testutil.CreateTestRedisClient(t)
	model := tenant.NewTenantModel(redisClient)
	handlers := tenant.NewHandlers(logger, redisClient, "")
	router := setupRouter(handlers)

	t.Run("should return 404 when there's no tenant", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should delete tenant", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := tenant.Tenant{
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
		}
		model.Set(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/"+existingResource.ID, nil)
		router.ServeHTTP(w, req)
		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		// Test
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, true, response["success"])

		// Cleanup
		model.Clear(context.Background(), existingResource.ID)
	})
}
