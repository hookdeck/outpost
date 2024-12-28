package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	api "github.com/hookdeck/outpost/internal/services/api"
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

func TestSetTenantIDMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Parallel()

	t.Run("should set tenantID from param", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "tenantID", Value: "test_tenant"}}

		// Create a middleware chain
		var tenantID string
		handler := api.SetTenantIDMiddleware()
		nextHandler := func(c *gin.Context) {
			val, exists := c.Get("tenantID")
			if exists {
				tenantID = val.(string)
			}
		}

		// Test
		handler(c)
		nextHandler(c)

		assert.Equal(t, "test_tenant", tenantID)
	})

	t.Run("should not set tenantID when param is empty", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "tenantID", Value: ""}}

		// Create a middleware chain
		var tenantIDExists bool
		handler := api.SetTenantIDMiddleware()
		nextHandler := func(c *gin.Context) {
			_, tenantIDExists = c.Get("tenantID")
		}

		// Test
		handler(c)
		nextHandler(c)

		assert.False(t, tenantIDExists)
	})

	t.Run("should not set tenantID when param is missing", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		// Create a middleware chain
		var tenantIDExists bool
		handler := api.SetTenantIDMiddleware()
		nextHandler := func(c *gin.Context) {
			_, tenantIDExists = c.Get("tenantID")
		}

		// Test
		handler(c)
		nextHandler(c)

		assert.False(t, tenantIDExists)
	})
}

func TestAPIKeyOrTenantJWTAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Parallel()

	const jwtSecret = "jwt_secret"
	const apiKey = "api_key"
	const tenantID = "test_tenant"

	t.Run("should reject when JWT tenantID doesn't match param", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "tenantID", Value: "different_tenant"}}

		// Create JWT token for tenantID
		token, err := api.JWT.New(jwtSecret, tenantID)
		if err != nil {
			t.Fatal(err)
		}

		// Set auth header
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Authorization", "Bearer "+token)

		// Test
		handler := api.APIKeyOrTenantJWTAuthMiddleware(apiKey, jwtSecret)
		handler(c)

		assert.Equal(t, http.StatusUnauthorized, c.Writer.Status())
	})

	t.Run("should accept when JWT tenantID matches param", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "tenantID", Value: tenantID}}

		// Create JWT token for tenantID
		token, err := api.JWT.New(jwtSecret, tenantID)
		if err != nil {
			t.Fatal(err)
		}

		// Set auth header
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Authorization", "Bearer "+token)

		// Create a middleware chain
		var contextTenantID string
		handler := api.APIKeyOrTenantJWTAuthMiddleware(apiKey, jwtSecret)
		nextHandler := func(c *gin.Context) {
			val, exists := c.Get("tenantID")
			if exists {
				contextTenantID = val.(string)
			}
		}

		// Test
		handler(c)
		if c.Writer.Status() == http.StatusUnauthorized {
			t.Fatal("handler returned unauthorized")
		}
		nextHandler(c)

		assert.Equal(t, tenantID, contextTenantID)
	})

	t.Run("should accept when using API key regardless of tenantID param", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "tenantID", Value: "any_tenant"}}

		// Set auth header with API key
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Authorization", "Bearer "+apiKey)

		// Test
		handler := api.APIKeyOrTenantJWTAuthMiddleware(apiKey, jwtSecret)
		handler(c)

		assert.NotEqual(t, http.StatusUnauthorized, c.Writer.Status())
	})
}

func TestTenantJWTAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Parallel()

	const jwtSecret = "jwt_secret"
	const tenantID = "test_tenant"

	t.Run("should reject when JWT tenantID doesn't match param", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "tenantID", Value: "different_tenant"}}

		// Create JWT token for tenantID
		token, err := api.JWT.New(jwtSecret, tenantID)
		if err != nil {
			t.Fatal(err)
		}

		// Set auth header
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Authorization", "Bearer "+token)

		// Test
		handler := api.TenantJWTAuthMiddleware(jwtSecret)
		handler(c)

		assert.Equal(t, http.StatusUnauthorized, c.Writer.Status())
	})

	t.Run("should accept when JWT tenantID matches param", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "tenantID", Value: tenantID}}

		// Create JWT token for tenantID
		token, err := api.JWT.New(jwtSecret, tenantID)
		if err != nil {
			t.Fatal(err)
		}

		// Set auth header
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Authorization", "Bearer "+token)

		// Test
		handler := api.TenantJWTAuthMiddleware(jwtSecret)
		handler(c)

		assert.Equal(t, http.StatusOK, c.Writer.Status())

		// Verify tenantID was set in context
		contextTenantID, exists := c.Get("tenantID")
		assert.True(t, exists)
		assert.Equal(t, tenantID, contextTenantID)
	})

	t.Run("should accept when no tenantID param", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		// Create JWT token for tenantID
		token, err := api.JWT.New(jwtSecret, tenantID)
		if err != nil {
			t.Fatal(err)
		}

		// Set auth header
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Authorization", "Bearer "+token)

		// Test
		handler := api.TenantJWTAuthMiddleware(jwtSecret)
		handler(c)

		assert.Equal(t, http.StatusOK, c.Writer.Status())

		// Verify tenantID was set in context
		contextTenantID, exists := c.Get("tenantID")
		assert.True(t, exists)
		assert.Equal(t, tenantID, contextTenantID)
	})

	t.Run("should reject invalid token", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		// Set auth header with invalid token
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("Authorization", "Bearer invalid.token")

		// Test
		handler := api.TenantJWTAuthMiddleware(jwtSecret)
		handler(c)

		assert.Equal(t, http.StatusUnauthorized, c.Writer.Status())
	})

	t.Run("should reject missing token", func(t *testing.T) {
		t.Parallel()

		// Setup
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		// Test
		handler := api.TenantJWTAuthMiddleware(jwtSecret)
		handler(c)

		assert.Equal(t, http.StatusBadRequest, c.Writer.Status())
	})
}
