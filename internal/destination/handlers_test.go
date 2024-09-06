package destination_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/destination"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func setupRouter(destinationHandlers *destination.DestinationHandlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.GET("/destinations", destinationHandlers.List)
	r.POST("/destinations", destinationHandlers.Create)
	r.GET("/destinations/:destinationID", destinationHandlers.Retrieve)
	r.PATCH("/destinations/:destinationID", destinationHandlers.Update)
	r.DELETE("/destinations/:destinationID", destinationHandlers.Delete)
	return r
}

func TestDestinationListHandler(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	handlers := destination.NewHandlers(redisClient)
	router := setupRouter(handlers)

	t.Run("should return 501", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/destinations", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotImplemented, w.Code)
	})
}

func TestDestinationCreateHandler(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	handlers := destination.NewHandlers(redisClient)
	router := setupRouter(handlers)

	t.Run("should create", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()

		exampleDestination := destination.CreateDestinationRequest{
			Name: "Test Destination",
		}
		destinationJSON, _ := json.Marshal(exampleDestination)
		req, _ := http.NewRequest("POST", "/destinations", strings.NewReader(string(destinationJSON)))
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, exampleDestination.Name, destinationResponse["name"])
		assert.NotEqual(t, "", destinationResponse["id"])
	})
}

func TestDestinationRetrieveHandler(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	handlers := destination.NewHandlers(redisClient)
	router := setupRouter(handlers)

	t.Run("should return 404 when there's no destination", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/destinations/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should retrieve when there's a destination", func(t *testing.T) {
		t.Parallel()

		// Setup test destination
		exampleDestination := destination.Destination{
			ID:   uuid.New().String(),
			Name: "Test Destination",
		}
		redisClient.Set(context.Background(), "destination:"+exampleDestination.ID, exampleDestination.Name, 0)

		// Test HTTP request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/destinations/"+exampleDestination.ID, nil)
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, exampleDestination.ID, destinationResponse["id"])
		assert.Equal(t, exampleDestination.Name, destinationResponse["name"])

		// Clean up
		redisClient.Del(context.Background(), "destination:"+exampleDestination.ID)
	})
}

func TestDestinationUpdateHandler(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)
	handlers := destination.NewHandlers(redisClient)
	router := setupRouter(handlers)

	initialDestination := destination.Destination{
		Name: "Test Destination",
	}

	updateDestinationRequest := destination.UpdateDestinationRequest{
		Name: "Updated Destination",
	}
	updateDestinationJSON, _ := json.Marshal(updateDestinationRequest)

	t.Run("should validate", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PATCH", "/destinations/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("should return 404 when there's no destination", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PATCH", "/destinations/invalid_id", strings.NewReader(string(updateDestinationJSON)))
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should update destination", func(t *testing.T) {
		t.Parallel()

		// Setup initial destination
		newDestination := initialDestination
		newDestination.ID = uuid.New().String()
		redisClient.Set(context.Background(), "destination:"+newDestination.ID, newDestination.Name, 0)

		// Test HTTP request
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PATCH", "/destinations/"+newDestination.ID, strings.NewReader(string(updateDestinationJSON)))
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusAccepted, w.Code)
		assert.Equal(t, newDestination.ID, destinationResponse["id"])
		assert.Equal(t, updateDestinationRequest.Name, destinationResponse["name"])

		// Clean up
		redisClient.Del(context.Background(), "destination:"+newDestination.ID)
	})
}

func TestDestinationDeleteHandler(t *testing.T) {
	redisClient := testutil.CreateTestRedisClient(t)
	handlers := destination.NewHandlers(redisClient)
	router := setupRouter(handlers)

	t.Run("should return 404 when there's no destination", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/destinations/invalid_id", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should delete destination", func(t *testing.T) {
		t.Parallel()

		// Setup initial destination
		newDestination := destination.Destination{
			ID:   uuid.New().String(),
			Name: "Test Destination",
		}
		redisClient.Set(context.Background(), "destination:"+newDestination.ID, newDestination.Name, 0)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/destinations/"+newDestination.ID, nil)
		router.ServeHTTP(w, req)

		var destinationResponse map[string]any
		json.Unmarshal(w.Body.Bytes(), &destinationResponse)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, newDestination.ID, destinationResponse["id"])
		assert.Equal(t, newDestination.Name, destinationResponse["name"])
	})
}
