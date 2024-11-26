package destinationmockserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/models"
)

func NewRouter(entityStore EntityStore) http.Handler {
	r := gin.Default()

	handlers := Handlers{
		entityStore: entityStore,
	}

	r.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	r.GET("/destinations", handlers.ListDestination)
	r.PUT("/destinations", handlers.UpsertDestination)
	r.DELETE("/destinations/:destinationID", handlers.DeleteDestination)

	r.POST("/webhook/:destinationID", handlers.ReceiveWebhookEvent)

	r.GET("/destinations/:destinationID/events", handlers.ListEvent)

	return r.Handler()
}

type Handlers struct {
	entityStore EntityStore
}

func (h *Handlers) ListDestination(c *gin.Context) {
	c.JSON(http.StatusOK, h.entityStore.ListDestination(c.Request.Context()))
}

func (h *Handlers) UpsertDestination(c *gin.Context) {
	var input models.Destination
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.entityStore.UpsertDestination(c.Request.Context(), input); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, input)
}

func (h *Handlers) DeleteDestination(c *gin.Context) {
	h.entityStore.DeleteDestination(c.Request.Context(), c.Param("destinationID"))
	c.Status(http.StatusOK)
}

func (h *Handlers) ReceiveWebhookEvent(c *gin.Context) {
	destinationID := c.Param("destinationID")
	destination := h.entityStore.RetrieveDestination(c.Request.Context(), destinationID)
	if destination == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "destination not found"})
		return
	}

	var input map[string]interface{}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	h.entityStore.ReceiveEvent(c.Request.Context(), destinationID, input)

	c.Status(http.StatusOK)
}

func (h *Handlers) ListEvent(c *gin.Context) {
	destinationID := c.Param("destinationID")
	destination := h.entityStore.RetrieveDestination(c.Request.Context(), destinationID)
	if destination == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "destination not found"})
		return
	}
	c.JSON(http.StatusOK, h.entityStore.ListEvent(c.Request.Context(), destinationID))
}
