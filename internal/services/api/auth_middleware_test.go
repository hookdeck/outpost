package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublicRouter(t *testing.T) {
	t.Parallel()

	const apiKey = ""
	router, _, _ := setupTestRouter(t, apiKey, "")

	t.Run("should accept requests without a token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/healthz", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should accept requests with an invalid authorization token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/healthz", nil)
		req.Header.Set("Authorization", "invalid key")
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should accept requests with a valid authorization token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/healthz", nil)
		req.Header.Set("Authorization", "Bearer key")
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestPrivateAPIKeyRouter(t *testing.T) {
	t.Parallel()

	const apiKey = "key"
	router, _, _ := setupTestRouter(t, apiKey, "")

	t.Run("should reject requests without a token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/tenant_id", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should reject requests with an malformed authorization header", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/tenant_id", nil)
		req.Header.Set("Authorization", "invalid key")
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("should reject requests with an incorrect authorization token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/tenant_id", nil)
		req.Header.Set("Authorization", "Bearer invalid")
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should accept requests with a valid authorization token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", baseAPIPath+"/tenant_id", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
	})
}
