package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	ErrInvalidBearerToken = errors.New("invalid token")
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

func APIKeyAuthMiddleware(apiKey string) gin.HandlerFunc {
	if apiKey == "" {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		authorizationToken, err := extractBearerToken(c.GetHeader("Authorization"))
		if err != nil {
			AbortWithError(c, http.StatusBadRequest, ErrInvalidBearerToken)
			return
		}
		if authorizationToken != apiKey {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}

func APIKeyOrTenantJWTAuthMiddleware(apiKey string, jwtKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authorizationToken, err := extractBearerToken(c.GetHeader("Authorization"))
		if err != nil {
			AbortWithError(c, http.StatusBadRequest, ErrInvalidBearerToken)
			return
		}
		if authorizationToken == apiKey {
			c.Next()
			return
		}

		tokenTenantID, err := JWT.ExtractTenantID(jwtKey, authorizationToken)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// If tenantID param exists, verify it matches token
		if paramTenantID := c.Param("tenantID"); paramTenantID != "" && paramTenantID != tokenTenantID {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// Set tenantID in context
		c.Set("tenantID", tokenTenantID)
		c.Next()
	}
}

func extractBearerToken(header string) (string, error) {
	if header == "" {
		return "", nil
	}
	if !strings.HasPrefix(header, "Bearer ") {
		return "", errors.New("invalid bearer token")
	}
	return strings.TrimPrefix(header, "Bearer "), nil
}
