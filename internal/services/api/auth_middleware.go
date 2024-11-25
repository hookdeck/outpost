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
		tenantID := c.Param("tenantID")
		valid, err := JWT.Verify(jwtKey, authorizationToken, tenantID)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if !valid {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
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
