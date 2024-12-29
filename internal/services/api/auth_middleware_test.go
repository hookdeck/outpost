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

func newJWTToken(t *testing.T, secret string, tenantID string) string {
	token, err := api.JWT.New(secret, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestTenantJWTAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Parallel()

	tests := []struct {
		name          string
		apiKey        string
		jwtSecret     string
		header        string
		paramTenantID string
		wantStatus    int
		wantTenantID  string
	}{
		{
			name:       "should return 404 when apiKey is empty",
			apiKey:     "",
			jwtSecret:  "secret",
			header:     "Bearer token",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "should return 404 when jwtSecret is empty",
			apiKey:     "key",
			jwtSecret:  "",
			header:     "Bearer token",
			wantStatus: http.StatusNotFound,
		},
		{
			name:         "should return 401 when no auth header",
			apiKey:       "key",
			jwtSecret:    "secret",
			wantStatus:   http.StatusUnauthorized,
			wantTenantID: "",
		},
		{
			name:         "should return 400 when invalid auth header",
			apiKey:       "key",
			jwtSecret:    "secret",
			header:       "invalid",
			wantStatus:   http.StatusBadRequest,
			wantTenantID: "",
		},
		{
			name:         "should return 401 when invalid token",
			apiKey:       "key",
			jwtSecret:    "secret",
			header:       "Bearer invalid",
			wantStatus:   http.StatusUnauthorized,
			wantTenantID: "",
		},
		{
			name:         "should return 200 when valid token",
			apiKey:       "key",
			jwtSecret:    "secret",
			header:       "Bearer " + newJWTToken(t, "secret", "tenant-id"),
			wantStatus:   http.StatusOK,
			wantTenantID: "tenant-id",
		},
		{
			name:          "should return 401 when tenantID param doesn't match token",
			apiKey:        "key",
			jwtSecret:     "secret",
			header:        "Bearer " + newJWTToken(t, "secret", "tenant-id"),
			paramTenantID: "other-tenant-id",
			wantStatus:    http.StatusUnauthorized,
			wantTenantID:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				c.Request.Header.Set("Authorization", tt.header)
			}
			if tt.paramTenantID != "" {
				c.Params = []gin.Param{{Key: "tenantID", Value: tt.paramTenantID}}
			}

			handler := api.TenantJWTAuthMiddleware(tt.apiKey, tt.jwtSecret)
			handler(c)

			t.Logf("Test case: %s, Expected: %d, Got: %d", tt.name, tt.wantStatus, w.Code)
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantTenantID != "" {
				tenantID, exists := c.Get("tenantID")
				assert.True(t, exists)
				assert.Equal(t, tt.wantTenantID, tenantID)
			}
		})
	}
}

func TestAuthRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Parallel()

	t.Run("APIKeyAuthMiddleware", func(t *testing.T) {
		t.Run("should set RoleAdmin when apiKey is empty", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

			handler := api.APIKeyAuthMiddleware("")
			var role string
			nextHandler := func(c *gin.Context) {
				val, exists := c.Get("authRole")
				if exists {
					role = val.(string)
				}
			}

			handler(c)
			nextHandler(c)

			assert.Equal(t, api.RoleAdmin, role)
		})

		t.Run("should set RoleAdmin when valid API key", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			c.Request.Header.Set("Authorization", "Bearer key")

			handler := api.APIKeyAuthMiddleware("key")
			var role string
			nextHandler := func(c *gin.Context) {
				val, exists := c.Get("authRole")
				if exists {
					role = val.(string)
				}
			}

			handler(c)
			nextHandler(c)

			assert.Equal(t, api.RoleAdmin, role)
		})
	})

	t.Run("APIKeyOrTenantJWTAuthMiddleware", func(t *testing.T) {
		t.Run("should set RoleAdmin when apiKey is empty", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

			handler := api.APIKeyOrTenantJWTAuthMiddleware("", "jwt_secret")
			var role string
			nextHandler := func(c *gin.Context) {
				val, exists := c.Get("authRole")
				if exists {
					role = val.(string)
				}
			}

			handler(c)
			nextHandler(c)

			assert.Equal(t, api.RoleAdmin, role)
		})

		t.Run("should set RoleAdmin when using API key", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			c.Request.Header.Set("Authorization", "Bearer key")

			handler := api.APIKeyOrTenantJWTAuthMiddleware("key", "jwt_secret")
			var role string
			nextHandler := func(c *gin.Context) {
				val, exists := c.Get("authRole")
				if exists {
					role = val.(string)
				}
			}

			handler(c)
			nextHandler(c)

			assert.Equal(t, api.RoleAdmin, role)
		})

		t.Run("should set RoleTenant when using valid JWT", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			token := newJWTToken(t, "jwt_secret", "tenant-id")
			c.Request.Header.Set("Authorization", "Bearer "+token)

			handler := api.APIKeyOrTenantJWTAuthMiddleware("key", "jwt_secret")
			var role string
			nextHandler := func(c *gin.Context) {
				val, exists := c.Get("authRole")
				if exists {
					role = val.(string)
				}
			}

			handler(c)
			nextHandler(c)

			assert.Equal(t, api.RoleTenant, role)
		})
	})

	t.Run("TenantJWTAuthMiddleware", func(t *testing.T) {
		t.Run("should set RoleTenant when using valid JWT", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			token := newJWTToken(t, "jwt_secret", "tenant-id")
			c.Request.Header.Set("Authorization", "Bearer "+token)

			handler := api.TenantJWTAuthMiddleware("key", "jwt_secret")
			var role string
			nextHandler := func(c *gin.Context) {
				val, exists := c.Get("authRole")
				if exists {
					role = val.(string)
				}
			}

			handler(c)
			nextHandler(c)

			assert.Equal(t, api.RoleTenant, role)
		})

		t.Run("should not set role when apiKey is empty", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			token := newJWTToken(t, "jwt_secret", "tenant-id")
			c.Request.Header.Set("Authorization", "Bearer "+token)

			handler := api.TenantJWTAuthMiddleware("", "jwt_secret")
			var roleExists bool
			nextHandler := func(c *gin.Context) {
				_, roleExists = c.Get("authRole")
			}

			handler(c)
			nextHandler(c)

			assert.False(t, roleExists)
		})
	})
}
