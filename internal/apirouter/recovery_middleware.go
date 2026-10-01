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
		// A Content-Type the handler set before the panic would otherwise be kept.
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.AbortWithStatusJSON(http.StatusInternalServerError, ErrorResponse{
			Status:  http.StatusInternalServerError,
			Message: "internal server error",
		})
	})
}
