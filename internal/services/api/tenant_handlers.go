package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type TenantHandlers struct {
	logger       *otelzap.Logger
	jwtSecret    string
	metadataRepo models.MetadataRepo
}

func NewTenantHandlers(
	logger *otelzap.Logger,
	jwtSecret string,
	metadataRepo models.MetadataRepo,
) *TenantHandlers {
	return &TenantHandlers{
		logger:       logger,
		jwtSecret:    jwtSecret,
		metadataRepo: metadataRepo,
	}
}

func (h *TenantHandlers) Upsert(c *gin.Context) {
	logger := h.logger.Ctx(c.Request.Context())
	tenantID := c.Param("tenantID")

	// Check existing tenant.
	tenant, err := h.metadataRepo.RetrieveTenant(c.Request.Context(), tenantID)
	if err != nil {
		logger.Error("failed to get tenant", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}

	// If tenant already exists, return.
	if tenant != nil {
		c.JSON(http.StatusOK, tenant)
		return
	}

	// Create new tenant.
	tenant = &models.Tenant{
		ID:        tenantID,
		CreatedAt: time.Now(),
	}
	if err := h.metadataRepo.UpsertTenant(c.Request.Context(), *tenant); err != nil {
		logger.Error("failed to set tenant", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusCreated, tenant)
}

func (h *TenantHandlers) Retrieve(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	c.JSON(http.StatusOK, tenant)
}

func (h *TenantHandlers) Delete(c *gin.Context) {
	logger := h.logger.Ctx(c.Request.Context())
	tenantID := c.Param("tenantID")

	// Delete tenant.
	err := h.metadataRepo.DeleteTenant(c.Request.Context(), tenantID)
	if err != nil {
		logger.Error("failed to delete tenant", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}

	// TODO: move this logic into models.MetadataRepo.DeleteTenant
	// Delete associated destinations.
	destinations, err := h.metadataRepo.ListDestinationByTenant(c.Request.Context(), tenantID)
	if err != nil {
		logger.Error("failed to list destinations", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	if len(destinations) > 0 {
		destinationIDs := make([]string, len(destinations))
		for i, destination := range destinations {
			destinationIDs[i] = destination.ID
		}
		_, err = h.metadataRepo.DeleteManyDestination(c.Request.Context(), tenantID, destinationIDs...)
		if err != nil {
			logger.Error("failed to delete destinations", zap.Error(err))
			c.Status(http.StatusInternalServerError)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
	return
}

func (h *TenantHandlers) RetrievePortal(c *gin.Context) {
	logger := h.logger.Ctx(c.Request.Context())
	tenant := mustTenantFromContext(c)
	jwtToken, err := JWT.New(h.jwtSecret, tenant.ID)
	if err != nil {
		logger.Error("failed to create jwt token", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"redirect_url": "https://example.com?token=" + jwtToken,
	})
}
