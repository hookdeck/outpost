package apirouter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/stretchr/testify/assert"
)

func TestRecoveryMiddleware(t *testing.T) {
	serve := func(handler gin.HandlerFunc) *httptest.ResponseRecorder {
		router := gin.New()
		router.Use(apirouter.RecoveryMiddleware())
		router.GET("/", handler)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		return w
	}

	t.Run("panic returns the JSON error envelope", func(t *testing.T) {
		w := serve(func(c *gin.Context) {
			panic("test panic")
		})

		requireErrorResponse(t, w, http.StatusInternalServerError, "internal server error")
	})

	t.Run("panic after the handler set a content type still returns JSON", func(t *testing.T) {
		w := serve(func(c *gin.Context) {
			c.Header("Content-Type", "text/plain")
			panic("test panic")
		})

		requireErrorResponse(t, w, http.StatusInternalServerError, "internal server error")
		assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	})

	t.Run("panic after the response started leaves it as written", func(t *testing.T) {
		w := serve(func(c *gin.Context) {
			c.String(http.StatusOK, "partial")
			panic("test panic")
		})

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
		assert.Equal(t, "partial", w.Body.String())
	})
}
