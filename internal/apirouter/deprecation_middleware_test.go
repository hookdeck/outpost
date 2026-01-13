package apirouter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/stretchr/testify/assert"
)

func TestDeprecationWarningMiddleware(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	t.Run("should add deprecation headers", func(t *testing.T) {
		t.Parallel()

		router := gin.New()
		router.Use(apirouter.DeprecationWarningMiddleware())
		router.GET("/api/v1/:tenantID/destinations", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/my-tenant/destinations", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "true", w.Header().Get("Deprecation"))
		assert.Equal(t, "</api/v1/tenants/my-tenant/destinations>; rel=\"successor-version\"", w.Header().Get("Link"))
	})

	t.Run("should compute correct new path for tenant root", func(t *testing.T) {
		t.Parallel()

		router := gin.New()
		router.Use(apirouter.DeprecationWarningMiddleware())
		router.GET("/api/v1/:tenantID", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/tenant-123", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "true", w.Header().Get("Deprecation"))
		assert.Equal(t, "</api/v1/tenants/tenant-123>; rel=\"successor-version\"", w.Header().Get("Link"))
	})

	t.Run("should compute correct new path for nested resources", func(t *testing.T) {
		t.Parallel()

		router := gin.New()
		router.Use(apirouter.DeprecationWarningMiddleware())
		router.GET("/api/v1/:tenantID/destinations/:destinationID/events/:eventID", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/my-tenant/destinations/dest-1/events/evt-1", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "true", w.Header().Get("Deprecation"))
		assert.Equal(t, "</api/v1/tenants/my-tenant/destinations/dest-1/events/evt-1>; rel=\"successor-version\"", w.Header().Get("Link"))
	})
}
