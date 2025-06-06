package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/util/maputil"
)

type DestinationHandlers struct {
	logger      *logging.Logger
	telemetry   telemetry.Telemetry
	entityStore models.EntityStore
	topics      []string
	registry    destregistry.Registry
}

func NewDestinationHandlers(logger *logging.Logger, telemetry telemetry.Telemetry, entityStore models.EntityStore, topics []string, registry destregistry.Registry) *DestinationHandlers {
	return &DestinationHandlers{
		logger:      logger,
		telemetry:   telemetry,
		entityStore: entityStore,
		topics:      topics,
		registry:    registry,
	}
}

func (h *DestinationHandlers) List(c *gin.Context) {
	typeParams := c.QueryArray("type")
	topicsParams := c.QueryArray("topics")
	var opts models.ListDestinationByTenantOpts
	if len(typeParams) > 0 || len(topicsParams) > 0 {
		opts = models.WithDestinationFilter(models.DestinationFilter{
			Type:   typeParams,
			Topics: topicsParams,
		})
	}

	tenantID := mustTenantIDFromContext(c)
	if tenantID == "" {
		return
	}

	destinations, err := h.entityStore.ListDestinationByTenant(c.Request.Context(), tenantID, opts)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	// Convert destinations to display format
	displayDestinations := make([]*destregistry.DestinationDisplay, len(destinations))
	for i, dest := range destinations {
		display, err := h.registry.DisplayDestination(&dest)
		if err != nil {
			AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
			return
		}
		displayDestinations[i] = display
	}

	c.JSON(http.StatusOK, displayDestinations)
}

func (h *DestinationHandlers) Create(c *gin.Context) {
	var input CreateDestinationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		AbortWithValidationError(c, err)
		return
	}

	tenantID := mustTenantIDFromContext(c)
	if tenantID == "" {
		return
	}

	destination := input.ToDestination(tenantID)
	if err := destination.Validate(h.topics); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if err := h.registry.ValidateDestination(c.Request.Context(), &destination); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if err := h.registry.PreprocessDestination(&destination, nil, &destregistry.PreprocessDestinationOpts{
		Role: mustRoleFromContext(c),
	}); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if err := h.entityStore.CreateDestination(c.Request.Context(), destination); err != nil {
		h.handleUpsertDestinationError(c, err)
		return
	}
	h.telemetry.DestinationCreated(c.Request.Context(), destination.Type)

	display, err := h.registry.DisplayDestination(&destination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusCreated, display)
}

func (h *DestinationHandlers) Retrieve(c *gin.Context) {
	tenantID := mustTenantIDFromContext(c)
	if tenantID == "" {
		return
	}
	destination := h.mustRetrieveDestination(c, tenantID, c.Param("destinationID"))
	if destination == nil {
		return
	}

	display, err := h.registry.DisplayDestination(destination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, display)
}

func (h *DestinationHandlers) Update(c *gin.Context) {
	// Parse & validate request.
	var input UpdateDestinationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		AbortWithValidationError(c, err)
		return
	}

	// Retrieve destination.
	tenantID := mustTenantIDFromContext(c)
	if tenantID == "" {
		return
	}
	originalDestination := h.mustRetrieveDestination(c, tenantID, c.Param("destinationID"))
	if originalDestination == nil {
		return
	}

	updatedDestination := *originalDestination

	// Validate.
	if input.Topics != nil {
		updatedDestination.Topics = input.Topics
		if err := updatedDestination.Validate(h.topics); err != nil {
			AbortWithValidationError(c, err)
			return
		}
	}
	shouldRevalidate := false
	if input.Type != "" {
		shouldRevalidate = true
		updatedDestination.Type = input.Type
	}
	if input.Config != nil {
		shouldRevalidate = true
		updatedDestination.Config = maputil.MergeStringMaps(originalDestination.Config, input.Config)
	}
	if input.Credentials != nil {
		shouldRevalidate = true
		updatedDestination.Credentials = maputil.MergeStringMaps(originalDestination.Credentials, input.Credentials)
	}

	// Always preprocess before updating
	if err := h.registry.PreprocessDestination(&updatedDestination, originalDestination, &destregistry.PreprocessDestinationOpts{
		Role: mustRoleFromContext(c),
	}); err != nil {
		AbortWithValidationError(c, err)
		return
	}

	if shouldRevalidate {
		if err := h.registry.ValidateDestination(c.Request.Context(), &updatedDestination); err != nil {
			AbortWithValidationError(c, err)
			return
		}
	}

	// Update destination.
	if err := h.entityStore.UpsertDestination(c.Request.Context(), updatedDestination); err != nil {
		h.handleUpsertDestinationError(c, err)
		return
	}

	display, err := h.registry.DisplayDestination(&updatedDestination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, display)
}

func (h *DestinationHandlers) Delete(c *gin.Context) {
	tenantID := mustTenantIDFromContext(c)
	if tenantID == "" {
		return
	}
	destination := h.mustRetrieveDestination(c, tenantID, c.Param("destinationID"))
	if destination == nil {
		return
	}
	if err := h.entityStore.DeleteDestination(c.Request.Context(), destination.TenantID, destination.ID); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	display, err := h.registry.DisplayDestination(destination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, display)
}

func (h *DestinationHandlers) Disable(c *gin.Context) {
	h.setDisabilityHandler(c, true)
}

func (h *DestinationHandlers) Enable(c *gin.Context) {
	h.setDisabilityHandler(c, false)
}

func (h *DestinationHandlers) ListProviderMetadata(c *gin.Context) {
	metadata := h.registry.ListProviderMetadata()
	c.JSON(http.StatusOK, metadata)
}

func (h *DestinationHandlers) RetrieveProviderMetadata(c *gin.Context) {
	providerType := c.Param("type")
	metadata, err := h.registry.RetrieveProviderMetadata(providerType)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, metadata)
}

func (h *DestinationHandlers) setDisabilityHandler(c *gin.Context, disabled bool) {
	tenantID := mustTenantIDFromContext(c)
	if tenantID == "" {
		return
	}
	destination := h.mustRetrieveDestination(c, tenantID, c.Param("destinationID"))
	if destination == nil {
		return
	}
	shouldUpdate := false
	if disabled && destination.DisabledAt == nil {
		shouldUpdate = true
		now := time.Now()
		destination.DisabledAt = &now
	}
	if !disabled && destination.DisabledAt != nil {
		shouldUpdate = true
		destination.DisabledAt = nil
	}
	if shouldUpdate {
		if err := h.entityStore.UpsertDestination(c.Request.Context(), *destination); err != nil {
			h.handleUpsertDestinationError(c, err)
			return
		}
	}

	display, err := h.registry.DisplayDestination(destination)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, display)
}

func (h *DestinationHandlers) mustRetrieveDestination(c *gin.Context, tenantID, destinationID string) *models.Destination {
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		if errors.Is(err, models.ErrDestinationDeleted) {
			c.Status(http.StatusNotFound)
			return nil
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return nil
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return nil
	}
	return destination
}

func (h *DestinationHandlers) handleUpsertDestinationError(c *gin.Context, err error) {
	if strings.Contains(err.Error(), "validation failed") {
		AbortWithValidationError(c, err)
		return
	}
	if errors.Is(err, models.ErrDuplicateDestination) {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}
	AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
}

// ===== Requests =====

type CreateDestinationRequest struct {
	ID          string             `json:"id" binding:"-"`
	Type        string             `json:"type" binding:"required"`
	Topics      models.Topics      `json:"topics" binding:"required"`
	Config      models.Config      `json:"config" binding:"-"`
	Credentials models.Credentials `json:"credentials" binding:"-"`
}

func (r *CreateDestinationRequest) ToDestination(tenantID string) models.Destination {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	if r.Config == nil {
		r.Config = make(map[string]string)
	}
	if r.Credentials == nil {
		r.Credentials = make(map[string]string)
	}

	return models.Destination{
		ID:          r.ID,
		Type:        r.Type,
		Topics:      r.Topics,
		Config:      r.Config,
		Credentials: r.Credentials,
		CreatedAt:   time.Now(),
		DisabledAt:  nil,
		TenantID:    tenantID,
	}
}

type UpdateDestinationRequest struct {
	Type        string             `json:"type" binding:"-"`
	Topics      models.Topics      `json:"topics" binding:"-"`
	Config      models.Config      `json:"config" binding:"-"`
	Credentials models.Credentials `json:"credentials" binding:"-"`
}

func mustRoleFromContext(c *gin.Context) string {
	if role, exists := c.Get(authRoleKey); exists {
		if roleStr, ok := role.(string); ok {
			return roleStr
		}
	}
	return ""
}
