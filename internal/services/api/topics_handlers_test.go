package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestTopicsHandlers_List(t *testing.T) {
	t.Parallel()

	router, _, _ := setupTestRouter(t, "", "")

	t.Run("should return topics", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/topics", nil)
		router.ServeHTTP(w, req)

		marshaledTestTopics, _ := json.Marshal(testutil.TestTopics)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, string(marshaledTestTopics), w.Body.String())
	})
}
