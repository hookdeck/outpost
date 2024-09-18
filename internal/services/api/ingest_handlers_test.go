package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/stretchr/testify/assert"
)

func TestIngestHandlers(t *testing.T) {
	t.Parallel()

	router, _, _ := setupTestRouter(t, "", "")

	t.Run("should ingest events", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()

		testEvent := ingest.Event{
			ID:            uuid.New().String(),
			TenantID:      uuid.New().String(),
			DestinationID: uuid.New().String(),
			Topic:         "topic",
			Time:          time.Now(),
			Metadata:      map[string]string{"key": "value"},
			Data:          map[string]interface{}{"key": "value"},
		}
		testEventJSON, _ := json.Marshal(testEvent)
		req, _ := http.NewRequest("POST", "/publish", strings.NewReader(string(testEventJSON)))
		router.ServeHTTP(w, req)

		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}
