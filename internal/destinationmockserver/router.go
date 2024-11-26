package destinationmockserver

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/models"
)

func NewRouter() http.Handler {
	r := gin.Default()

	handlers := Handlers{}

	r.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	r.GET("/destinations", handlers.ListDestination)
	r.PUT("/destinations", handlers.UpsertDestination)
	r.DELETE("/destinations/:id", handlers.DeleteDestination)

	return r.Handler()
}

type Handlers struct{}

func (h *Handlers) ListDestination(c *gin.Context) {
	c.JSON(http.StatusOK, []models.Destination{})
}

func (h *Handlers) UpsertDestination(c *gin.Context) {
	var input models.Destination
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	log.Println(input)
	c.JSON(http.StatusOK, input)
}

func (h *Handlers) DeleteDestination(c *gin.Context) {
	c.Status(http.StatusOK)
}
