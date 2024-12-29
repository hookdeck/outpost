package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	ErrMissingAuthHeader  = errors.New("missing authorization header")
	ErrInvalidBearerToken = errors.New("invalid bearer token format")
	ErrTenantIDNotFound   = errors.New("tenantID not found in context")
)

const (
	// Context keys
	authRoleKey = "authRole"

	// Role values
	RoleAdmin  = "admin"
	RoleTenant = "tenant"
)

func SetTenantIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Param("tenantID")
		if tenantID != "" {
			c.Set("tenantID", tenantID)
		}
		c.Next()
	}
}

// validateAuthHeader checks the Authorization header and returns the token if valid
func validateAuthHeader(c *gin.Context) (string, error) {
	header := c.GetHeader("Authorization")
	if header == "" {
		return "", ErrMissingAuthHeader
	}
	if !strings.HasPrefix(header, "Bearer ") {
		return "", ErrInvalidBearerToken
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" {
		return "", ErrInvalidBearerToken
	}
	return token, nil
}

func APIKeyAuthMiddleware(apiKey string) gin.HandlerFunc {
	// When apiKey is empty, everything is admin-only through VPC
	if apiKey == "" {
		return func(c *gin.Context) {
			c.Set(authRoleKey, RoleAdmin)
			c.Next()
		}
	}

	return func(c *gin.Context) {
		token, err := validateAuthHeader(c)
		if err != nil {
			if errors.Is(err, ErrInvalidBearerToken) {
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		if token != apiKey {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set(authRoleKey, RoleAdmin)
		c.Next()
	}
}

func APIKeyOrTenantJWTAuthMiddleware(apiKey string, jwtKey string) gin.HandlerFunc {
	// When apiKey is empty, everything is admin-only through VPC
	if apiKey == "" {
		return func(c *gin.Context) {
			c.Set(authRoleKey, RoleAdmin)
			c.Next()
		}
	}

	return func(c *gin.Context) {
		token, err := validateAuthHeader(c)
		if err != nil {
			if errors.Is(err, ErrInvalidBearerToken) {
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// Try API key first
		if token == apiKey {
			c.Set(authRoleKey, RoleAdmin)
			c.Next()
			return
		}

		// Try JWT auth
		tokenTenantID, err := JWT.ExtractTenantID(jwtKey, token)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// If tenantID param exists, verify it matches token
		if paramTenantID := c.Param("tenantID"); paramTenantID != "" && paramTenantID != tokenTenantID {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("tenantID", tokenTenantID)
		c.Set(authRoleKey, RoleTenant)
		c.Next()
	}
}

func TenantJWTAuthMiddleware(apiKey string, jwtKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// When apiKey or jwtKey is empty, JWT-only routes should not exist
		if apiKey == "" || jwtKey == "" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		token, err := validateAuthHeader(c)
		if err != nil {
			if errors.Is(err, ErrInvalidBearerToken) {
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		tokenTenantID, err := JWT.ExtractTenantID(jwtKey, token)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// If tenantID param exists, verify it matches token
		if paramTenantID := c.Param("tenantID"); paramTenantID != "" && paramTenantID != tokenTenantID {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("tenantID", tokenTenantID)
		c.Set(authRoleKey, RoleTenant)
		c.Next()
	}
}

func mustTenantIDFromContext(c *gin.Context) string {
	tenantID, exists := c.Get("tenantID")
	if !exists {
		c.AbortWithStatus(http.StatusInternalServerError)
		return ""
	}
	if tenantID == nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return ""
	}
	return tenantID.(string)
}
