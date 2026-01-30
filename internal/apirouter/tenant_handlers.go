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
	"github.com/hookdeck/outpost/internal/tenantstore"
)

type TenantHandlers struct {
	logger       *logging.Logger
	telemetry    telemetry.Telemetry
	jwtSecret    string
	deploymentID string
	tenantStore  tenantstore.TenantStore
}

func NewTenantHandlers(
	logger *logging.Logger,
	telemetry telemetry.Telemetry,
	jwtSecret string,
	deploymentID string,
	tenantStore tenantstore.TenantStore,
) *TenantHandlers {
	return &TenantHandlers{
		logger:       logger,
		telemetry:    telemetry,
		jwtSecret:    jwtSecret,
		deploymentID: deploymentID,
		tenantStore:  tenantStore,
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
	existingTenant, err := h.tenantStore.RetrieveTenant(c.Request.Context(), tenantID)
	if err != nil && err != tenantstore.ErrTenantDeleted {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	// If tenant already exists, update it (PUT replaces metadata)
	if existingTenant != nil {
		existingTenant.Metadata = input.Metadata
		existingTenant.UpdatedAt = time.Now()
		if err := h.tenantStore.UpsertTenant(c.Request.Context(), *existingTenant); err != nil {
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
	if err := h.tenantStore.UpsertTenant(c.Request.Context(), *tenant); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	h.telemetry.TenantCreated(c.Request.Context())
	c.JSON(http.StatusCreated, tenant)
}

func (h *TenantHandlers) Retrieve(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	c.JSON(http.StatusOK, tenant)
}

func (h *TenantHandlers) List(c *gin.Context) {
	// Parse and validate cursors (next/prev are mutually exclusive)
	cursors, errResp := ParseCursors(c)
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}

	// Parse and validate dir (sort direction)
	dir, errResp := ParseDir(c)
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}

	req := tenantstore.ListTenantRequest{
		Next: cursors.Next,
		Prev: cursors.Prev,
		Dir:  dir,
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
	resp, err := h.tenantStore.ListTenant(c.Request.Context(), req)
	if err != nil {
		// Map errors to HTTP status codes
		if errors.Is(err, tenantstore.ErrListTenantNotSupported) {
			AbortWithError(c, http.StatusNotImplemented, ErrorResponse{
				Err:     err,
				Code:    http.StatusNotImplemented,
				Message: err.Error(),
			})
			return
		}
		if errors.Is(err, tenantstore.ErrConflictingCursors) {
			AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
			return
		}
		if errors.Is(err, tenantstore.ErrInvalidCursor) {
			AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
			return
		}
		if errors.Is(err, tenantstore.ErrInvalidOrder) {
			AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *TenantHandlers) Delete(c *gin.Context) {
	tenant := mustTenantFromContext(c)

	err := h.tenantStore.DeleteTenant(c.Request.Context(), tenant.ID)
	if err != nil {
		if err == tenantstore.ErrTenantNotFound {
			c.Status(http.StatusNotFound)
			return
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *TenantHandlers) RetrieveToken(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	jwtToken, err := JWT.New(h.jwtSecret, JWTClaims{
		TenantID:     tenant.ID,
		DeploymentID: h.deploymentID,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": jwtToken, "tenant_id": tenant.ID})
}

func (h *TenantHandlers) RetrievePortal(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	jwtToken, err := JWT.New(h.jwtSecret, JWTClaims{
		TenantID:     tenant.ID,
		DeploymentID: h.deploymentID,
	})
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
		"tenant_id":    tenant.ID,
	})
}
