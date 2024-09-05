package destination

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type DestinationHandlers struct {
	logger *otelzap.Logger
	model  *DestinationModel
}

func NewHandlers(logger *otelzap.Logger, redisClient *redis.Client) *DestinationHandlers {
	return &DestinationHandlers{
		logger: logger,
		model:  NewDestinationModel(redisClient),
	}
}

func (h *DestinationHandlers) List(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

func (h *DestinationHandlers) Create(c *gin.Context) {
	var json CreateDestinationRequest
	if err := c.ShouldBindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id := uuid.New().String()
	destination := Destination{
		ID:         id,
		Type:       json.Type,
		Topics:     json.Topics,
		CreatedAt:  time.Now(),
		DisabledAt: nil,
	}
	if err := h.model.Set(c.Request.Context(), destination); err != nil {
		h.logger.Ctx(c.Request.Context()).Error("failed to set destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusCreated, destination)
}

func (h *DestinationHandlers) Retrieve(c *gin.Context) {
	destinationID := c.Param("destinationID")
	destination, err := h.model.Get(c.Request.Context(), destinationID)
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
	destinationID := c.Param("destinationID")
	destination, err := h.model.Get(c.Request.Context(), destinationID)
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
	if err := h.model.Set(c.Request.Context(), *destination); err != nil {
		logger.Error("failed to set destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusAccepted, destination)
}

func (h *DestinationHandlers) Delete(c *gin.Context) {
	destinationID := c.Param("destinationID")
	destination, err := h.model.Clear(c.Request.Context(), destinationID)
	if err != nil {
		h.logger.Ctx(c.Request.Context()).Error("failed to clear destination", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, destination)
}
