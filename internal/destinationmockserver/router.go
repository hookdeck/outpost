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
	r.DELETE("/destinations/:id", handlers.DeleteDestination)

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
	h.entityStore.DeleteDestination(c.Request.Context(), c.Param("id"))
	c.Status(http.StatusOK)
}
