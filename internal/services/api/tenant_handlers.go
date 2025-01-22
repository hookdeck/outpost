package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
)

type TenantHandlers struct {
	logger      *logging.Logger
	jwtSecret   string
	entityStore models.EntityStore
}

func NewTenantHandlers(
	logger *logging.Logger,
	jwtSecret string,
	entityStore models.EntityStore,
) *TenantHandlers {
	return &TenantHandlers{
		logger:      logger,
		jwtSecret:   jwtSecret,
		entityStore: entityStore,
	}
}

func (h *TenantHandlers) Upsert(c *gin.Context) {
	tenantID := mustTenantIDFromContext(c)
	if tenantID == "" {
		return
	}

	// Check existing tenant.
	tenant, err := h.entityStore.RetrieveTenant(c.Request.Context(), tenantID)
	if err != nil && err != models.ErrTenantDeleted {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
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
		Topics:    []string{},
		CreatedAt: time.Now(),
	}
	if err := h.entityStore.UpsertTenant(c.Request.Context(), *tenant); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusCreated, tenant)
}

func (h *TenantHandlers) Retrieve(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	c.JSON(http.StatusOK, tenant)
}

func (h *TenantHandlers) Delete(c *gin.Context) {
	tenantID := mustTenantIDFromContext(c)
	if tenantID == "" {
		return
	}

	err := h.entityStore.DeleteTenant(c.Request.Context(), tenantID)
	if err != nil {
		if err == models.ErrTenantNotFound {
			c.Status(http.StatusNotFound)
			return
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
	return
}

func (h *TenantHandlers) RetrieveToken(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	jwtToken, err := JWT.New(h.jwtSecret, tenant.ID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": jwtToken})
}

func (h *TenantHandlers) RetrievePortal(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	jwtToken, err := JWT.New(h.jwtSecret, tenant.ID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}

	// Get theme from query parameter
	theme := c.Query("theme")
	if theme != "dark" && theme != "light" {
		theme = ""
	}

	portalURL := scheme + "://" + c.Request.Host + "?token=" + jwtToken
	if theme != "" {
		portalURL += "&theme=" + theme
	}

	c.JSON(http.StatusOK, gin.H{
		"redirect_url": portalURL,
	})
}
