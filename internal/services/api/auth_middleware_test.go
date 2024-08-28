package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupRouter(apiKey string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.Use(apiKeyAuthMiddleware(apiKey))
	r.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

func TestPublicRouter(t *testing.T) {
	t.Parallel()

	router := setupRouter("")

	t.Run("should accept requests without a token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should accept requests with an invalid authorization token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		req.Header.Set("Authorization", "invalid key")
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should accept requests with a valid authorization token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		req.Header.Set("Authorization", "Bearer key")
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestPrivateAPIKeyRouter(t *testing.T) {
	t.Parallel()

	const apiKey = "key"

	router := setupRouter(apiKey)

	t.Run("should reject requests without a token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should reject requests with an invalid authorization token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		req.Header.Set("Authorization", "invalid key")
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("should accept requests with a valid authorization token", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/healthz", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
