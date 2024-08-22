package api

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/EventKit/internal/destination"
)

func NewRouter() http.Handler {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		time.Sleep(5 * time.Second)
		log.Println("health")
		c.Status(http.StatusOK)
	})

	destinationHandlers := destination.NewHandlers()

	r.GET("/destinations", destinationHandlers.List)
	r.POST("/destinations", destinationHandlers.Create)
	r.GET("/destinations/:destinationID", destinationHandlers.Retrieve)
	r.PATCH("/destinations/:destinationID", destinationHandlers.Update)
	r.DELETE("/destinations/:destinationID", destinationHandlers.Delete)

	return r
}
