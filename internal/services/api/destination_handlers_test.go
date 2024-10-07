package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	api "github.com/hookdeck/EventKit/internal/services/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseTenantPath(id string) string {
	return baseAPIPath + "/" + id
}

func setupTestMetadataRepo(_ *testing.T, redisClient *redis.Client, cipher models.Cipher) models.MetadataRepo {
	if cipher == nil {
		cipher = models.NewAESCipher("secret")
	}
	return models.NewMetadataRepo(redisClient, cipher)
}

func setupExistingTenant(t *testing.T, metadataRepo models.MetadataRepo) string {
	tenant := models.Tenant{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
	err := metadataRepo.UpsertTenant(context.Background(), tenant)
	require.Nil(t, err)
	return tenant.ID
}

func TestDestinationListHandler(t *testing.T) {
	t.Parallel()

	router, _, redisClient := setupTestRouter(t, "", "")

	t.Run("should return empty list", func(t *testing.T) {
		t.Parallel()
		metadataRepo := setupTestMetadataRepo(t, redisClient, nil)
		tenantID := setupExistingTenant(t, metadataRepo)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseTenantPath(tenantID)+"/destinations", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `[]`, w.Body.String())
	})

	t.Run("should return list with existing destinations", func(t *testing.T) {
		t.Parallel()

		// Arrange
		metadataRepo := setupTestMetadataRepo(t, redisClient, nil)
		tenantID := setupExistingTenant(t, metadataRepo)
		inputDestination := models.Destination{
			Type:       "webhooks",
			Topics:     []string{"user.created", "user.updated"},
			Config:     map[string]string{"url": "https://example.com"},
			DisabledAt: nil,
			TenantID:   tenantID,
		}
		ids := make([]string, 5)
		timestamps := make([]time.Time, 5)
		for i := 0; i < 5; i++ {
			ids[i] = uuid.New().String()
			timestamps[i] = time.Now()
			inputDestination.ID = ids[i]
			inputDestination.CreatedAt = timestamps[i]
			metadataRepo.UpsertDestination(context.Background(), inputDestination)
		}

		// Act
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseTenantPath(tenantID)+"/destinations", nil)
		router.ServeHTTP(w, req)

		// Assert
		destinationResponse := []any{}
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)
		fmt.Println(len(destinationResponse))

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 5, len(destinationResponse))
		for i, destinationJSON := range destinationResponse {
			destination := destinationJSON.(map[string]any)
			assert.Equal(t, ids[i], destination["id"])
			assert.Equal(t, inputDestination.Type, destination["type"])
			assertMarshalEqual(t, inputDestination.Topics, destination["topics"])
			assert.Equal(t, timestamps[i].Format(time.RFC3339Nano), destination["created_at"])
		}
	})
}

func TestDestinationCreateHandler(t *testing.T) {
	t.Parallel()

	router, _, redisClient := setupTestRouter(t, "", "")
	metadataRepo := setupTestMetadataRepo(t, redisClient, nil)
	tenantID := setupExistingTenant(t, metadataRepo)

	t.Run("should create", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()

		exampleDestination := api.CreateDestinationRequest{
			Type:        "webhooks",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{"url": "https://example.com"},
			Credentials: map[string]string{},
		}
		destinationJSON, _ := json.Marshal(exampleDestination)
		req, _ := http.NewRequest("POST", baseTenantPath(tenantID)+"/destinations", strings.NewReader(string(destinationJSON)))
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, exampleDestination.Type, destinationResponse["type"])
		assertMarshalEqual(t, exampleDestination.Topics, destinationResponse["topics"])
		assert.NotEqual(t, "", destinationResponse["id"])
		assert.NotEqual(t, "", destinationResponse["created_at"])
	})

	t.Run("should do basic validation", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()

		exampleDestination := api.CreateDestinationRequest{
			Type: "webhooks",
		}
		destinationJSON, _ := json.Marshal(exampleDestination)
		req, _ := http.NewRequest("POST", baseTenantPath(tenantID)+"/destinations", strings.NewReader(string(destinationJSON)))
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, destinationResponse["error"], "Error:Field validation")
	})

	t.Run("should do destination validation", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()

		exampleDestination := api.CreateDestinationRequest{
			Type:        "webhooks",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{"invalid_config": "https://example.com"},
			Credentials: map[string]string{},
		}
		destinationJSON, _ := json.Marshal(exampleDestination)
		req, _ := http.NewRequest("POST", baseTenantPath(tenantID)+"/destinations", strings.NewReader(string(destinationJSON)))
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, destinationResponse["error"], "validation failed:")
	})
}

func TestDestinationRetrieveHandler(t *testing.T) {
	t.Parallel()

	router, _, redisClient := setupTestRouter(t, "", "")
	metadataRepo := setupTestMetadataRepo(t, redisClient, nil)
	tenantID := setupExistingTenant(t, metadataRepo)

	t.Run("should return 404 when there's no destination", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseTenantPath(tenantID)+"/destinations/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should retrieve when there's a destination", func(t *testing.T) {
		t.Parallel()

		// Setup test destination
		exampleDestination := models.Destination{
			ID:          uuid.New().String(),
			Type:        "webhooks",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{"url": "https://example.com"},
			Credentials: map[string]string{},
			CreatedAt:   time.Now(),
			TenantID:    tenantID,
		}
		metadataRepo.UpsertDestination(context.Background(), exampleDestination)

		// Test HTTP request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseTenantPath(tenantID)+"/destinations/"+exampleDestination.ID, nil)
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, exampleDestination.ID, destinationResponse["id"])
		assert.Equal(t, exampleDestination.Type, destinationResponse["type"])
		assertMarshalEqual(t, exampleDestination.Topics, destinationResponse["topics"])
		assert.Equal(t, exampleDestination.CreatedAt.Format(time.RFC3339Nano), destinationResponse["created_at"])

		// Clean up
		redisClient.Del(context.Background(), "destination:"+exampleDestination.ID)
	})
}

func TestDestinationUpdateHandler(t *testing.T) {
	t.Parallel()

	router, _, redisClient := setupTestRouter(t, "", "")
	metadataRepo := setupTestMetadataRepo(t, redisClient, nil)
	tenantID := setupExistingTenant(t, metadataRepo)

	initialDestination := models.Destination{
		Type:        "webhooks",
		Topics:      []string{"user.created", "user.updated"},
		Config:      map[string]string{"url": "https://example.com"},
		Credentials: map[string]string{},
		CreatedAt:   time.Now(),
		TenantID:    tenantID,
	}

	updateDestinationRequest := api.UpdateDestinationRequest{
		Topics: []string{"*"},
	}
	updateDestinationJSON, _ := json.Marshal(updateDestinationRequest)

	t.Run("should validate", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PATCH", baseTenantPath(tenantID)+"/destinations/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("should return 404 when there's no destination", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PATCH", baseTenantPath(tenantID)+"/destinations/invalid_id", strings.NewReader(string(updateDestinationJSON)))
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should validate destination", func(t *testing.T) {
		t.Parallel()

		destination := initialDestination
		destination.ID = uuid.New().String()
		metadataRepo.UpsertDestination(context.Background(), destination)

		invalidRequest := api.UpdateDestinationRequest{
			Type: "invalid",
		}
		invalidRequestJSON, _ := json.Marshal(invalidRequest)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PATCH", baseTenantPath(tenantID)+"/destinations/"+destination.ID, strings.NewReader(string(invalidRequestJSON)))
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("should update destination", func(t *testing.T) {
		t.Parallel()

		// Setup initial destination
		destination := initialDestination
		destination.ID = uuid.New().String()
		metadataRepo.UpsertDestination(context.Background(), destination)

		// Test HTTP request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PATCH", baseTenantPath(tenantID)+"/destinations/"+destination.ID, strings.NewReader(string(updateDestinationJSON)))
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, destination.ID, destinationResponse["id"])
		assert.Equal(t, destination.Type, destinationResponse["type"])
		assertMarshalEqual(t, updateDestinationRequest.Topics, destinationResponse["topics"])
		assert.Equal(t, destination.CreatedAt.Format(time.RFC3339Nano), destinationResponse["created_at"])

		// Clean up
		redisClient.Del(context.Background(), "destination:"+destination.ID)
	})

	t.Run("should update destination type", func(t *testing.T) {
		t.Parallel()

		// Setup initial destination
		destination := initialDestination
		destination.ID = uuid.New().String()
		metadataRepo.UpsertDestination(context.Background(), destination)

		// Test HTTP request
		updated := api.UpdateDestinationRequest{
			Type: "rabbitmq",
			Config: map[string]string{
				"server_url": "localhost:5672",
				"exchange":   "events",
			},
			Credentials: map[string]string{
				"username": "guest",
				"password": "guest",
			},
		}
		updatedJSON, _ := json.Marshal(updated)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PATCH", baseTenantPath(tenantID)+"/destinations/"+destination.ID, strings.NewReader(string(updatedJSON)))
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		log.Println(destinationResponse)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, destination.ID, destinationResponse["id"])
		assert.Equal(t, updated.Type, destinationResponse["type"])
		assertMarshalEqual(t, updated.Config, destinationResponse["config"])
		assertMarshalEqual(t, updated.Credentials, destinationResponse["credentials"])
		assert.Equal(t, destination.CreatedAt.Format(time.RFC3339Nano), destinationResponse["created_at"])

		// Clean up
		redisClient.Del(context.Background(), "destination:"+destination.ID)
	})

	// TODO: add test for updating config & credentials
}

func TestDestinationDeleteHandler(t *testing.T) {
	t.Parallel()

	router, _, redisClient := setupTestRouter(t, "", "")
	metadataRepo := setupTestMetadataRepo(t, redisClient, nil)
	tenantID := setupExistingTenant(t, metadataRepo)

	t.Run("should return 404 when there's no destination", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", baseTenantPath(tenantID)+"/destinations/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should delete destination", func(t *testing.T) {
		t.Parallel()

		// Setup initial destination
		newDestination := models.Destination{
			ID:          uuid.New().String(),
			Type:        "webhooks",
			Topics:      []string{"user.created", "user.updated"},
			Config:      map[string]string{"url": "https://example.com"},
			Credentials: map[string]string{},
			CreatedAt:   time.Now(),
			TenantID:    tenantID,
		}
		metadataRepo.UpsertDestination(context.Background(), newDestination)

		// Test HTTP request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", baseTenantPath(tenantID)+"/destinations/"+newDestination.ID, nil)
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, newDestination.ID, destinationResponse["id"])
		assert.Equal(t, newDestination.Type, destinationResponse["type"])
		assertMarshalEqual(t, newDestination.Topics, destinationResponse["topics"])
		assert.Equal(t, newDestination.CreatedAt.Format(time.RFC3339Nano), destinationResponse["created_at"])
	})
}

// assertMarshalEqual asserts two value by marshalling them to JSON and comparing the result.
func assertMarshalEqual(t *testing.T, expected any, actual any, msgAndArgs ...interface{}) {
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err, "failed to marshal value: %v", expected)
	}
	actualJSON, _ := json.Marshal(actual)
	if err != nil {
		t.Fatal(err, "failed to marshal value: %v", actual)
	}
	assert.Equal(t, string(expectedJSON), string(actualJSON), msgAndArgs...)
}
