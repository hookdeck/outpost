package destinationmockserver

import (
	"net/http"
	"strings"

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
	if destinations, err := h.entityStore.ListDestination(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	} else {
		c.JSON(http.StatusOK, destinations)
	}
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
	if err := h.entityStore.DeleteDestination(c.Request.Context(), c.Param("destinationID")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

func (h *Handlers) ReceiveWebhookEvent(c *gin.Context) {
	destinationID := c.Param("destinationID")
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), destinationID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if destination == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "destination not found"})
		return
	}

	var input map[string]interface{}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	metadata := map[string]string{}
	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if strings.HasPrefix(lowerKey, "x-outpost") {
			metadata[strings.TrimPrefix(lowerKey, "x-outpost-")] = values[0]
		}
	}

	if event, err := h.entityStore.ReceiveEvent(c.Request.Context(), destinationID, input, metadata); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	} else {
		if event.Success {
			c.JSON(http.StatusOK, event)
		} else {
			c.JSON(http.StatusBadRequest, event)
		}
	}
}

func (h *Handlers) ListEvent(c *gin.Context) {
	destinationID := c.Param("destinationID")
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), destinationID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if destination == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "destination not found"})
		return
	}
	if events, err := h.entityStore.ListEvent(c.Request.Context(), destinationID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	} else {
		c.JSON(http.StatusOK, events)
	}
}
