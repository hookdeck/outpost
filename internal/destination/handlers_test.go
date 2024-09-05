package destination_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/destination"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

var tenantID = uuid.New().String()

func baseTenantPath(id string) string {
	return "/" + id
}

func setupRouter(destinationHandlers *destination.DestinationHandlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.GET("/:tenantID/destinations", destinationHandlers.List)
	r.POST("/:tenantID/destinations", destinationHandlers.Create)
	r.GET("/:tenantID/destinations/:destinationID", destinationHandlers.Retrieve)
	r.PATCH("/:tenantID/destinations/:destinationID", destinationHandlers.Update)
	r.DELETE("/:tenantID/destinations/:destinationID", destinationHandlers.Delete)
	return r
}

func TestDestinationListHandler(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	logger := testutil.CreateTestLogger(t)
	handlers := destination.NewHandlers(logger, redisClient)
	router := setupRouter(handlers)

	t.Run("should return 501", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseTenantPath(tenantID)+"/destinations", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotImplemented, w.Code)
	})
}

func TestDestinationCreateHandler(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	logger := testutil.CreateTestLogger(t)
	handlers := destination.NewHandlers(logger, redisClient)
	router := setupRouter(handlers)

	t.Run("should create", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()

		exampleDestination := destination.CreateDestinationRequest{
			Type:   "webhooks",
			Topics: []string{"user.created", "user.updated"},
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
}

func TestDestinationRetrieveHandler(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	logger := testutil.CreateTestLogger(t)
	handlers := destination.NewHandlers(logger, redisClient)
	model := destination.NewDestinationModel(redisClient)
	router := setupRouter(handlers)

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
		exampleDestination := destination.Destination{
			ID:        uuid.New().String(),
			Type:      "webhooks",
			Topics:    []string{"user.created", "user.updated"},
			CreatedAt: time.Now(),
		}
		model.Set(context.Background(), exampleDestination)

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

	redisClient := testutil.CreateTestRedisClient(t)
	logger := testutil.CreateTestLogger(t)
	handlers := destination.NewHandlers(logger, redisClient)
	model := destination.NewDestinationModel(redisClient)
	router := setupRouter(handlers)

	initialDestination := destination.Destination{
		ID:        uuid.New().String(),
		Type:      "webhooks",
		Topics:    []string{"user.created", "user.updated"},
		CreatedAt: time.Now(),
	}

	updateDestinationRequest := destination.UpdateDestinationRequest{
		Type: "not-webhooks",
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

	t.Run("should update destination", func(t *testing.T) {
		t.Parallel()

		// Setup initial destination
		model.Set(context.Background(), initialDestination)

		// Test HTTP request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PATCH", baseTenantPath(tenantID)+"/destinations/"+initialDestination.ID, strings.NewReader(string(updateDestinationJSON)))
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusAccepted, w.Code)
		assert.Equal(t, initialDestination.ID, destinationResponse["id"])
		assert.Equal(t, updateDestinationRequest.Type, destinationResponse["type"])
		assertMarshalEqual(t, updateDestinationRequest.Topics, destinationResponse["topics"])
		assert.Equal(t, initialDestination.CreatedAt.Format(time.RFC3339Nano), destinationResponse["created_at"])

		// Clean up
		redisClient.Del(context.Background(), "destination:"+initialDestination.ID)
	})
}

func TestDestinationDeleteHandler(t *testing.T) {
	redisClient := testutil.CreateTestRedisClient(t)
	logger := testutil.CreateTestLogger(t)
	handlers := destination.NewHandlers(logger, redisClient)
	model := destination.NewDestinationModel(redisClient)
	router := setupRouter(handlers)

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
		newDestination := destination.Destination{
			ID:        uuid.New().String(),
			Type:      "webhooks",
			Topics:    []string{"user.created", "user.updated"},
			CreatedAt: time.Now(),
		}
		model.Set(context.Background(), newDestination)

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
