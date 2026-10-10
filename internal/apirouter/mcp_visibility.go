package apirouter

import (
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/models"
)

// API v1 predates MCP Events: its clients can't render mcp destinations, so
// v1 leaves them, and their attempts, out of every response.

// v1HiddenDestinationTypes are the destination types API v1 never returns.
var v1HiddenDestinationTypes = []string{models.DestinationTypeMCP}

// hiddenInRequest reports whether the request's API version hides
// destinations of type typ.
func hiddenInRequest(c *gin.Context, typ string) bool {
	return apiVersionFromContext(c) < apiV2 && slices.Contains(v1HiddenDestinationTypes, typ)
}
