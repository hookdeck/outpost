package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
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
		h.logger.Ctx(c.Request.Context()).Error("failed to list destinations", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, destinations)
}

func (h *DestinationHandlers) Create(c *gin.Context) {
	var json CreateDestinationRequest
	if err := c.ShouldBindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id := uuid.New().String()
	destination := models.Destination{
		ID:          id,
		Type:        json.Type,
		Topics:      json.Topics,
		Config:      json.Config,
		Credentials: json.Credentials,
		CreatedAt:   time.Now(),
		DisabledAt:  nil,
		TenantID:    c.Param("tenantID"),
	}
	if err := destination.ValidateTopics(h.topics); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.entityStore.UpsertDestination(c.Request.Context(), destination); err != nil {
		if strings.Contains(err.Error(), "validation failed") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		h.logger.Ctx(c.Request.Context()).Error("failed to set destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusCreated, destination)
}

func (h *DestinationHandlers) Retrieve(c *gin.Context) {
	tenantID := c.Param("tenantID")
	destinationID := c.Param("destinationID")
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, destination)
}

func (h *DestinationHandlers) Update(c *gin.Context) {
	logger := h.logger.Ctx(c.Request.Context())

	// Parse & validate request.
	var json UpdateDestinationRequest
	if err := c.ShouldBindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get destination.
	tenantID := c.Param("tenantID")
	destinationID := c.Param("destinationID")
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		logger.Error("failed to get destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}

	// Update destination
	if json.Type != "" {
		destination.Type = json.Type
	}
	if json.Topics != nil {
		destination.Topics = json.Topics
		if err := destination.ValidateTopics(h.topics); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if json.Config != nil {
		destination.Config = json.Config
	}
	if json.Credentials != nil {
		destination.Credentials = json.Credentials
	}
	if err := h.entityStore.UpsertDestination(c.Request.Context(), *destination); err != nil {
		if strings.Contains(err.Error(), "validation failed") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		logger.Error("failed to set destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, destination)
}

func (h *DestinationHandlers) Delete(c *gin.Context) {
	tenantID := c.Param("tenantID")
	destinationID := c.Param("destinationID")
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		h.logger.Ctx(c.Request.Context()).Error("failed to get destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}
	err = h.entityStore.DeleteDestination(c.Request.Context(), tenantID, destinationID)
	if err != nil {
		h.logger.Ctx(c.Request.Context()).Error("failed to clear destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, destination)
}

// ===== Requests =====

type CreateDestinationRequest struct {
	Type        string            `json:"type" binding:"required"`
	Topics      models.Topics     `json:"topics" binding:"required"`
	Config      map[string]string `json:"config" binding:"required"`
	Credentials map[string]string `json:"credentials" binding:"required"`
}

type UpdateDestinationRequest struct {
	Type        string            `json:"type" binding:"-"`
	Topics      models.Topics     `json:"topics" binding:"-"`
	Config      map[string]string `json:"config" binding:"-"`
	Credentials map[string]string `json:"credentials" binding:"-"`
}
