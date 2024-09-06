package api

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type DestinationHandlers struct {
	logger      *otelzap.Logger
	redisClient *redis.Client
	model       *models.DestinationModel
}

func NewDestinationHandlers(logger *otelzap.Logger, redisClient *redis.Client, model *models.DestinationModel) *DestinationHandlers {
	return &DestinationHandlers{
		logger:      logger,
		redisClient: redisClient,
		model:       model,
	}
}

// TODO: support type & topics params
func (h *DestinationHandlers) List(c *gin.Context) {
	destinations, err := h.model.List(c.Request.Context(), h.redisClient, c.Param("tenantID"))
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
		ID:         id,
		Type:       json.Type,
		Topics:     json.Topics,
		CreatedAt:  time.Now(),
		DisabledAt: nil,
		TenantID:   c.Param("tenantID"),
	}
	if err := h.model.Set(c.Request.Context(), h.redisClient, destination); err != nil {
		h.logger.Ctx(c.Request.Context()).Error("failed to set destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusCreated, destination)
}

func (h *DestinationHandlers) Retrieve(c *gin.Context) {
	tenantID := c.Param("tenantID")
	destinationID := c.Param("destinationID")
	destination, err := h.model.Get(c.Request.Context(), h.redisClient, destinationID, tenantID)
	if err != nil {
		log.Println(err)
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
	destination, err := h.model.Get(c.Request.Context(), h.redisClient, destinationID, tenantID)
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
	destination.Type = json.Type
	destination.Topics = json.Topics
	if err := h.model.Set(c.Request.Context(), h.redisClient, *destination); err != nil {
		logger.Error("failed to set destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, destination)
}

func (h *DestinationHandlers) Delete(c *gin.Context) {
	tenantID := c.Param("tenantID")
	destinationID := c.Param("destinationID")
	destination, err := h.model.Get(c.Request.Context(), h.redisClient, destinationID, tenantID)
	if err != nil {
		h.logger.Ctx(c.Request.Context()).Error("failed to get destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}
	err = h.model.Clear(c.Request.Context(), h.redisClient, destinationID, tenantID)
	if err != nil {
		h.logger.Ctx(c.Request.Context()).Error("failed to clear destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, destination)
}

// ===== Requests =====

type CreateDestinationRequest struct {
	Type   string   `json:"type" binding:"required"`
	Topics []string `json:"topics" binding:"required"`
}

type UpdateDestinationRequest struct {
	Type   string   `json:"type" binding:"-"`
	Topics []string `json:"topics" binding:"-"`
}
