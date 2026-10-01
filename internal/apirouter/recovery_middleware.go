package apirouter

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RecoveryMiddleware recovers from a panic and answers with a JSON 500.
func RecoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, _ any) {
		if c.Writer.Written() {
			c.Abort()
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, ErrorResponse{
			Status:  http.StatusInternalServerError,
			Message: "internal server error",
		})
	})
}
