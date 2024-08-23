package destination

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type DestinationHandlers struct{}

func NewHandlers() *DestinationHandlers {
	return &DestinationHandlers{}
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
		ID:   id,
		Name: json.Name,
	}
	if err := SetDestination(c.Request.Context(), destination); err != nil {
		log.Println(err)
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":   id,
		"name": json.Name,
	})
}

func (h *DestinationHandlers) Retrieve(c *gin.Context) {
	destinationID := c.Param("destinationID")
	destination, err := GetDestination(c.Request.Context(), destinationID)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusInternalServerError)
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":   destination.ID,
		"name": destination.Name,
	})
}

func (h *DestinationHandlers) Update(c *gin.Context) {
	// Parse & validate request.
	var json UpdateDestinationRequest
	if err := c.ShouldBindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get destination.
	destinationID := c.Param("destinationID")
	destination, err := GetDestination(c.Request.Context(), destinationID)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusInternalServerError)
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}

	// Update destination
	destination.Name = json.Name
	if err := SetDestination(c.Request.Context(), *destination); err != nil {
		log.Println(err)
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"id":   destination.ID,
		"name": destination.Name,
	})
}

func (h *DestinationHandlers) Delete(c *gin.Context) {
	destinationID := c.Param("destinationID")
	destination, err := ClearDestination(c.Request.Context(), destinationID)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusInternalServerError)
		return
	}
	if destination == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":   destination.ID,
		"name": destination.Name,
	})
}
