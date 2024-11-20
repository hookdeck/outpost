package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

type DestinationHandlers struct {
	logger      *otelzap.Logger
	entityStore models.EntityStore
	topics      []string
}

func NewDestinationHandlers(logger *otelzap.Logger, entityStore models.EntityStore, topics []string) *DestinationHandlers {
	return &DestinationHandlers{
		logger:      logger,
		entityStore: entityStore,
		topics:      topics,
	}
}

// TODO: support type & topics params
func (h *DestinationHandlers) List(c *gin.Context) {
	destinations, err := h.entityStore.ListDestinationByTenant(c.Request.Context(), c.Param("tenantID"))
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, destinations)
}

func (h *DestinationHandlers) Create(c *gin.Context) {
	var input CreateDestinationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	destination := input.ToDestination(c.Param("tenantID"))
	if err := destination.Topics.Validate(h.topics); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if err := destination.Validate(c.Request.Context()); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	if err := h.entityStore.CreateDestination(c.Request.Context(), destination); err != nil {
		h.handleUpsertDestinationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, destination)
}

func (h *DestinationHandlers) Retrieve(c *gin.Context) {
	tenantID := c.Param("tenantID")
	destinationID := c.Param("destinationID")
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, destination)
}

func (h *DestinationHandlers) Update(c *gin.Context) {
	// Parse & validate request.
	var input UpdateDestinationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		AbortWithValidationError(c, err)
		return
	}

	// Get destination.
	tenantID := c.Param("tenantID")
	destinationID := c.Param("destinationID")
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}

	// Validate.
	if input.Topics != nil {
		destination.Topics = input.Topics
		if err := destination.Topics.Validate(h.topics); err != nil {
			AbortWithValidationError(c, err)
			return
		}
	}
	shouldRevalidate := false
	if input.Type != "" {
		shouldRevalidate = true
		destination.Type = input.Type
	}
	if input.Config != nil {
		shouldRevalidate = true
		destination.Config = input.Config
	}
	if input.Credentials != nil {
		shouldRevalidate = true
		destination.Credentials = input.Credentials
	}
	if shouldRevalidate {
		if err := destination.Validate(c.Request.Context()); err != nil {
			AbortWithValidationError(c, err)
			return
		}
	}

	// Update destination.
	if err := h.entityStore.UpsertDestination(c.Request.Context(), *destination); err != nil {
		h.handleUpsertDestinationError(c, err)
		return
	}
	c.JSON(http.StatusOK, destination)
}

func (h *DestinationHandlers) Delete(c *gin.Context) {
	tenantID := c.Param("tenantID")
	destinationID := c.Param("destinationID")
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}
	err = h.entityStore.DeleteDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, destination)
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
	ID          string            `json:"id" binding:"-"`
	Type        string            `json:"type" binding:"required"`
	Topics      models.Topics     `json:"topics" binding:"required"`
	Config      map[string]string `json:"config" binding:"required"`
	Credentials map[string]string `json:"credentials" binding:"-"`
}

func (r *CreateDestinationRequest) ToDestination(tenantID string) models.Destination {
	if r.ID == "" {
		r.ID = uuid.New().String()
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
	Type        string            `json:"type" binding:"-"`
	Topics      models.Topics     `json:"topics" binding:"-"`
	Config      map[string]string `json:"config" binding:"-"`
	Credentials map[string]string `json:"credentials" binding:"-"`
}
