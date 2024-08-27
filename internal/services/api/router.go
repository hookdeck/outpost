package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/destination"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func NewRouter(cfg *config.Config, logger *otelzap.Logger, redisClient *redis.Client) http.Handler {
	r := gin.Default()
	r.Use(otelgin.Middleware(cfg.Hostname))

	r.GET("/healthz", func(c *gin.Context) {
		logger.Ctx(c.Request.Context()).Info("health check")
		c.Status(http.StatusOK)
	})

	destinationHandlers := destination.NewHandlers(redisClient)

	r.GET("/destinations", destinationHandlers.List)
	r.POST("/destinations", destinationHandlers.Create)
	r.GET("/destinations/:destinationID", destinationHandlers.Retrieve)
	r.PATCH("/destinations/:destinationID", destinationHandlers.Update)
	r.DELETE("/destinations/:destinationID", destinationHandlers.Delete)

	return r
}
