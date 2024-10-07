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
	err := h.metadataRepo.DeleteTenant(c.Request.Context(), tenantID)
	if err != nil {
		logger.Error("failed to delete tenant", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
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

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}

	portalURL := scheme + "://" + c.Request.Host + "?token=" + jwtToken

	c.JSON(http.StatusOK, gin.H{
		"redirect_url": portalURL,
	})
}
