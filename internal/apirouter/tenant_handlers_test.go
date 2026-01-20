package apirouter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestDestinationUpsertHandler(t *testing.T) {
	t.Parallel()

	router, _, redisClient := setupTestRouter(t, "", "")
	entityStore := setupTestEntityStore(t, redisClient, nil)

	t.Run("should create when there's no existing tenant", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()

		id := idgen.String()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/tenants/"+id, nil)
		router.ServeHTTP(w, req)

		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, id, response["id"])
		assert.NotEqual(t, "", response["created_at"])
		assert.NotEqual(t, "", response["updated_at"])
		assert.Equal(t, response["created_at"], response["updated_at"])
	})

	t.Run("should return tenant when there's already one", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/tenants/"+existingResource.ID, nil)
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
		// Compare at second precision since Redis stores Unix timestamps
		assert.Equal(t, existingResource.CreatedAt.Unix(), createdAt.Unix())

		// Cleanup
		entityStore.DeleteTenant(context.Background(), existingResource.ID)
	})
}

func TestTenantRetrieveHandler(t *testing.T) {
	t.Parallel()

	router, _, redisClient := setupTestRouter(t, "", "")
	entityStore := setupTestEntityStore(t, redisClient, nil)

	t.Run("should return 404 when there's no tenant", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should retrieve tenant", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+existingResource.ID, nil)
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
		// Compare at second precision since Redis stores Unix timestamps
		assert.Equal(t, existingResource.CreatedAt.Unix(), createdAt.Unix())

		// Cleanup
		entityStore.DeleteTenant(context.Background(), existingResource.ID)
	})
}

func TestTenantDeleteHandler(t *testing.T) {
	t.Parallel()

	router, _, redisClient := setupTestRouter(t, "", "")
	entityStore := setupTestEntityStore(t, redisClient, nil)

	t.Run("should return 404 when there's no tenant", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", baseAPIPath+"/tenants/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should delete tenant", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", baseAPIPath+"/tenants/"+existingResource.ID, nil)
		router.ServeHTTP(w, req)
		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		// Test
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, true, response["success"])
	})

	t.Run("should delete tenant and associated destinations", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)
		inputDestination := models.Destination{
			Type:       "webhook",
			Topics:     []string{"user.created", "user.updated"},
			DisabledAt: nil,
			TenantID:   existingResource.ID,
		}
		ids := make([]string, 5)
		for i := 0; i < 5; i++ {
			ids[i] = idgen.String()
			inputDestination.ID = ids[i]
			inputDestination.CreatedAt = time.Now()
			entityStore.UpsertDestination(context.Background(), inputDestination)
		}

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", baseAPIPath+"/tenants/"+existingResource.ID, nil)
		router.ServeHTTP(w, req)
		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		// Test
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, true, response["success"])

		destinations, err := entityStore.ListDestinationByTenant(context.Background(), existingResource.ID)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(destinations))
	})
}

func TestTenantRetrieveTokenHandler(t *testing.T) {
	t.Parallel()

	apiKey := "api_key"
	jwtSecret := "jwt_secret"
	router, _, redisClient := setupTestRouter(t, apiKey, jwtSecret)
	entityStore := setupTestEntityStore(t, redisClient, nil)

	t.Run("should return token and tenant_id", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+existingResource.ID+"/token", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)
		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		// Test
		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotEmpty(t, response["token"])
		assert.Equal(t, existingResource.ID, response["tenant_id"])

		// Cleanup
		entityStore.DeleteTenant(context.Background(), existingResource.ID)
	})
}

func TestTenantRetrievePortalHandler(t *testing.T) {
	t.Parallel()

	apiKey := "api_key"
	jwtSecret := "jwt_secret"
	router, _, redisClient := setupTestRouter(t, apiKey, jwtSecret)
	entityStore := setupTestEntityStore(t, redisClient, nil)

	t.Run("should return redirect_url with token and tenant_id in body", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+existingResource.ID+"/portal", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)
		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		// Test
		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotEmpty(t, response["redirect_url"])
		assert.Contains(t, response["redirect_url"], "token=")
		assert.Equal(t, existingResource.ID, response["tenant_id"])

		// Cleanup
		entityStore.DeleteTenant(context.Background(), existingResource.ID)
	})

	t.Run("should include theme in redirect_url when provided", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+existingResource.ID+"/portal?theme=dark", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)
		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		// Test
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, response["redirect_url"], "token=")
		assert.Contains(t, response["redirect_url"], "theme=dark")
		assert.Equal(t, existingResource.ID, response["tenant_id"])

		// Cleanup
		entityStore.DeleteTenant(context.Background(), existingResource.ID)
	})
}

func TestTenantListHandler(t *testing.T) {
	t.Parallel()

	router, _, redisClient := setupTestRouter(t, "", "")
	_ = setupTestEntityStore(t, redisClient, nil)

	// Note: These tests use miniredis which doesn't support RediSearch.
	// The ListTenant feature requires RediSearch, so we expect 501 Not Implemented.

	t.Run("should return 501 when RediSearch is not available", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotImplemented, w.Code)

		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Contains(t, response["message"], "not enabled")
	})

	t.Run("should return 400 for invalid limit", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants?limit=notanumber", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Contains(t, response["message"], "invalid limit")
	})
}
