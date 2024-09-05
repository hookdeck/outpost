package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
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
			// TODO: Consider sending a more detailed error message.
			// Currently we don't have clear specs on how to send back error message.
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if authorizationToken != apiKey {
			// TODO: Consider sending a more detailed error message.
			// Currently we don't have clear specs on how to send back error message.
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
			// TODO: Consider sending a more detailed error message.
			// Currently we don't have clear specs on how to send back error message.
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if authorizationToken == apiKey {
			c.Next()
			return
		}
		tenantID := c.Param("tenantID")
		valid, err := JWT.Verify(jwtKey, authorizationToken, tenantID)
		if err != nil {
			// TODO: Consider sending a more detailed error message.
			// Currently we don't have clear specs on how to send back error message.
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if !valid {
			// TODO: Consider sending a more detailed error message.
			// Currently we don't have clear specs on how to send back error message.
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
