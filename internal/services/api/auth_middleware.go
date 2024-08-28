package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func apiKeyAuthMiddleware(apiKey string) gin.HandlerFunc {
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

func extractBearerToken(header string) (string, error) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", errors.New("invalid bearer token")
	}
	return strings.TrimPrefix(header, "Bearer "), nil
}
