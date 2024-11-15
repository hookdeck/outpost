package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
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

		id := uuid.New().String()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+id, nil)
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
		existingResource := models.Tenant{
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/"+existingResource.ID, nil)
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
		req, _ := http.NewRequest("GET", baseAPIPath+"/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should retrieve tenant", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := models.Tenant{
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+existingResource.ID, nil)
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
		req, _ := http.NewRequest("DELETE", baseAPIPath+"/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should delete tenant", func(t *testing.T) {
		t.Parallel()

		// Setup
		existingResource := models.Tenant{
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", baseAPIPath+"/"+existingResource.ID, nil)
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
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
		}
		entityStore.UpsertTenant(context.Background(), existingResource)
		inputDestination := models.Destination{
			Type:       "webhooks",
			Topics:     []string{"user.created", "user.updated"},
			DisabledAt: nil,
			TenantID:   existingResource.ID,
		}
		ids := make([]string, 5)
		for i := 0; i < 5; i++ {
			ids[i] = uuid.New().String()
			inputDestination.ID = ids[i]
			inputDestination.CreatedAt = time.Now()
			entityStore.UpsertDestination(context.Background(), inputDestination)
		}

		// Request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", baseAPIPath+"/"+existingResource.ID, nil)
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
