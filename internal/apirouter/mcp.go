package apirouter

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/models"
)

type MCPVerifier interface {
	PurgeTenant(ctx context.Context, tenantID string) error
}

func (h *DestinationHandlers) mcpEnabled() bool {
	_, err := h.registry.RetrieveProviderMetadata(models.DestinationTypeMCP)
	return err == nil
}

// abortMCPUseSubscribe refuses a create or update of an mcp destination:
// subscriptions are managed through the subscribe endpoints.
func abortMCPUseSubscribe(c *gin.Context) {
	AbortWithError(c, http.StatusUnprocessableEntity, ErrorResponse{
		Code:    http.StatusUnprocessableEntity,
		Message: "mcp destinations can't be created or updated here; use POST /tenants/" + c.Param("tenant_id") + "/mcp/subscribe",
	})
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

var errExpiresAtUnsupported = errors.New("expires_at is not supported for this destination type")
