package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
)

func TestWebhookHandling(t *testing.T) {
	server := NewServer(Config{
		EventTTL: 1 * time.Hour,
		MaxSize:  100,
	})

	t.Run("Webhook receives event with ID header", func(t *testing.T) {
		// Create a request with a sample payload and header
		payload := map[string]interface{}{
			"test": "value",
			"nested": map[string]interface{}{
				"field": 123,
			},
		}
		payloadBytes, _ := json.Marshal(payload)

		req, err := http.NewRequest("POST", "/webhook", bytes.NewBuffer(payloadBytes))
		assert.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-outpost-event-id", "test-event-123")

		// Record the response
		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.handleWebhook)
		handler.ServeHTTP(rr, req)

		// Check status code
		assert.Equal(t, http.StatusOK, rr.Code)

		// Parse and verify response
		var response map[string]interface{}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, true, response["received"])
		assert.Equal(t, "test-event-123", response["id"])

		// Verify the event was stored
		event, found := server.events.Get("test-event-123")
		assert.True(t, found)
		assert.Equal(t, "test-event-123", event.ID)
		assert.Equal(t, "value", event.Payload["test"])
		assert.NotNil(t, event.Payload["nested"])
		assert.Equal(t, "application/json", event.Headers["Content-Type"])
	})

	t.Run("Webhook generates ID if not provided", func(t *testing.T) {
		payload := map[string]interface{}{"field": "value"}
		payloadBytes, _ := json.Marshal(payload)

		req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer(payloadBytes))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.handleWebhook)
		handler.ServeHTTP(rr, req)

		var response map[string]interface{}
		json.Unmarshal(rr.Body.Bytes(), &response)
		assert.Equal(t, true, response["received"])
		assert.NotEmpty(t, response["id"])

		// ID should start with "event-"
		id := response["id"].(string)
		assert.Contains(t, id, "event-")

		// Verify storage
		_, found := server.events.Get(id)
		assert.True(t, found)
	})
}

func TestEventRetrieval(t *testing.T) {
	server := NewServer(Config{
		EventTTL: 1 * time.Hour,
		MaxSize:  100,
	})

	// Add a test event directly to the cache
	testEvent := &EventRecord{
		ID:         "test-event-456",
		ReceivedAt: time.Now(),
		Payload:    map[string]interface{}{"field": "value"},
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
	server.events.Add("test-event-456", testEvent)

	t.Run("Get existing event", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/events/test-event-456", nil)
		rr := httptest.NewRecorder()

		// Need to use the router to get URL params
		router := mux.NewRouter()
		router.HandleFunc("/events/{eventId}", server.getEvent).Methods("GET")
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var responseEvent EventRecord
		json.Unmarshal(rr.Body.Bytes(), &responseEvent)
		assert.Equal(t, "test-event-456", responseEvent.ID)
		assert.Equal(t, "value", responseEvent.Payload["field"])
	})

	t.Run("Get non-existent event", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/events/non-existent", nil)
		rr := httptest.NewRecorder()

		router := mux.NewRouter()
		router.HandleFunc("/events/{eventId}", server.getEvent).Methods("GET")
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHealthCheck(t *testing.T) {
	server := NewServer(Config{
		EventTTL: 1 * time.Hour,
		MaxSize:  100,
	})

	// Simulate receiving some events
	server.stats.EventsReceived = 150
	server.stats.EventsStored = 120

	req, _ := http.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	handler := http.HandlerFunc(server.healthCheck)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &response)
	assert.Equal(t, "healthy", response["status"])
	assert.Equal(t, float64(150), response["events_received"])
	assert.Equal(t, float64(120), response["events_stored"])
	assert.GreaterOrEqual(t, response["uptime_seconds"].(float64), float64(0))
}
