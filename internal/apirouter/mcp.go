package apirouter

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
)

type MCPVerifier interface {
	PurgeTenant(ctx context.Context, tenantID string) error
}

func (h *DestinationHandlers) mcpEnabled() bool {
	_, err := h.registry.RetrieveProviderMetadata(models.DestinationTypeMCP)
	return err == nil
}

func abortMCPAdminOnly(c *gin.Context) {
	AbortWithError(c, http.StatusForbidden, ErrorResponse{
		Code:    http.StatusForbidden,
		Message: "mcp destinations can only be created and updated with API key authentication",
	})
}

func validateExpiresAt(destType string, expiresAt, now time.Time) error {
	if destType != models.DestinationTypeMCP {
		return errors.New("expires_at is not supported for this destination type")
	}
	if !expiresAt.After(now) {
		return errors.New("expires_at must be in the future")
	}
	return nil
}

var mcpImmutableConfig = []string{"url", "principal", "arguments"}

func checkMCPImmutable(original, updated *models.Destination) error {
	var details []destregistry.ValidationErrorDetail
	for _, key := range mcpImmutableConfig {
		if original.Config[key] != updated.Config[key] {
			details = append(details, destregistry.ValidationErrorDetail{Field: "config." + key, Type: "immutable"})
		}
	}
	if !slices.Equal(original.Topics.Normalize(), updated.Topics.Normalize()) {
		details = append(details, destregistry.ValidationErrorDetail{Field: "topics", Type: "immutable"})
	}
	if len(details) == 0 {
		return nil
	}
	return destregistry.NewErrDestinationValidation(details)
}
