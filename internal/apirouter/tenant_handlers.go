package apirouter

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/telemetry"
)

type TenantHandlers struct {
	logger      *logging.Logger
	telemetry   telemetry.Telemetry
	jwtSecret   string
	entityStore models.EntityStore
}

func NewTenantHandlers(
	logger *logging.Logger,
	telemetry telemetry.Telemetry,
	jwtSecret string,
	entityStore models.EntityStore,
) *TenantHandlers {
	return &TenantHandlers{
		logger:      logger,
		telemetry:   telemetry,
		jwtSecret:   jwtSecret,
		entityStore: entityStore,
	}
}

func (h *TenantHandlers) Upsert(c *gin.Context) {
	tenantID := mustTenantIDFromContext(c)
	if tenantID == "" {
		return
	}

	// Parse request body for metadata
	var input struct {
		Metadata models.Metadata `json:"metadata,omitempty"`
	}
	// Only attempt to parse JSON if there's a request body
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&input); err != nil {
			AbortWithValidationError(c, err)
			return
		}
	}

	// Check existing tenant.
	existingTenant, err := h.entityStore.RetrieveTenant(c.Request.Context(), tenantID)
	if err != nil && err != models.ErrTenantDeleted {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	// If tenant already exists, update it (PUT replaces metadata)
	if existingTenant != nil {
		existingTenant.Metadata = input.Metadata
		existingTenant.UpdatedAt = time.Now()
		if err := h.entityStore.UpsertTenant(c.Request.Context(), *existingTenant); err != nil {
			AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
			return
		}
		c.JSON(http.StatusOK, existingTenant)
		return
	}

	// Create new tenant.
	now := time.Now()
	tenant := &models.Tenant{
		ID:        tenantID,
		Topics:    []string{},
		Metadata:  input.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.entityStore.UpsertTenant(c.Request.Context(), *tenant); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	h.telemetry.TenantCreated(c.Request.Context())
	c.JSON(http.StatusCreated, tenant)
}

func (h *TenantHandlers) Retrieve(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	c.JSON(http.StatusOK, tenant)
}

func (h *TenantHandlers) List(c *gin.Context) {
	// Parse query parameters
	req := models.ListTenantRequest{
		Next:  c.Query("next"),
		Prev:  c.Query("prev"),
		Order: c.Query("order"),
	}

	// Parse limit if provided
	if limitStr := c.Query("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(errors.New("invalid limit: must be an integer")))
			return
		}
		req.Limit = limit
	}

	// Call entity store
	resp, err := h.entityStore.ListTenant(c.Request.Context(), req)
	if err != nil {
		// Map errors to HTTP status codes
		if errors.Is(err, models.ErrListTenantNotSupported) {
			AbortWithError(c, http.StatusNotImplemented, ErrorResponse{
				Code:    http.StatusNotImplemented,
				Message: err.Error(),
			})
			return
		}
		if errors.Is(err, models.ErrConflictingCursors) {
			AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
			return
		}
		if errors.Is(err, models.ErrInvalidCursor) {
			AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
			return
		}
		if errors.Is(err, models.ErrInvalidOrder) {
			AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	c.JSON(http.StatusOK, resp)
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
	c.JSON(http.StatusOK, gin.H{"token": jwtToken, "tenant_id": tenant.ID})
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

	portalURL := scheme + "://" + c.Request.Host + "?token=" + jwtToken + "&tenant_id=" + tenant.ID
	if theme != "" {
		portalURL += "&theme=" + theme
	}

	c.JSON(http.StatusOK, gin.H{
		"redirect_url": portalURL,
		"tenant_id":    tenant.ID,
	})
}
