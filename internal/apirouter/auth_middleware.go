package apirouter

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	ErrMissingAuthHeader  = errors.New("missing authorization header")
	ErrInvalidBearerToken = errors.New("invalid bearer token format")
)

const (
	// Context keys
	authRoleKey = "authRole"

	// Role values
	RoleAdmin  = "admin"
	RoleTenant = "tenant"
)

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

func AuthenticatedMiddleware(apiKey string, jwtKey string) gin.HandlerFunc {
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
		claims, err := JWT.Extract(jwtKey, token)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// If tenantID param exists, verify it matches token
		if paramTenantID := c.Param("tenantID"); paramTenantID != "" && paramTenantID != claims.TenantID {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("tenantID", claims.TenantID)
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
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		claims, err := JWT.Extract(jwtKey, token)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// If tenantID param exists, verify it matches token
		if paramTenantID := c.Param("tenantID"); paramTenantID != "" && paramTenantID != claims.TenantID {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("tenantID", claims.TenantID)
		c.Set(authRoleKey, RoleTenant)
		c.Next()
	}
}
