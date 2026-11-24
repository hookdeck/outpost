package apirouter

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type MCPVerifier interface {
	PurgeTenant(ctx context.Context, tenantID string) error
}

// abortMCPUnavailable answers 503 while the configured event definitions are
// unusable; the reason goes to the request log only.
func abortMCPUnavailable(c *gin.Context, cause error) {
	AbortWithError(c, http.StatusServiceUnavailable, ErrorResponse{
		Code:    http.StatusServiceUnavailable,
		Message: "MCP Events unavailable",
		Err:     cause,
	})
}
