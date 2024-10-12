package api

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/EventKit/internal/deliverymq"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/portal"
	"github.com/hookdeck/EventKit/internal/publishmq"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

type RouterConfig struct {
	Hostname  string
	APIKey    string
	JWTSecret string
}

func NewRouter(
	cfg RouterConfig,
	logger *otelzap.Logger,
	redisClient *redis.Client,
	deliveryMQ *deliverymq.DeliveryMQ,
	entityStore models.EntityStore,
	publishmqEventHandler publishmq.EventHandler,
) http.Handler {
	r := gin.Default()
	r.Use(otelgin.Middleware(cfg.Hostname))
	r.Use(MetricsMiddleware())

	portal.AddRoutes(r)

	apiRouter := r.Group("/api/v1")

	apiRouter.GET("/healthz", func(c *gin.Context) {
		log.Println("/healthz")
		time.Sleep(1 * time.Second)
		c.Status(http.StatusOK)
	})

	tenantHandlers := NewTenantHandlers(logger, cfg.JWTSecret, entityStore)
	destinationHandlers := NewDestinationHandlers(logger, entityStore)
	publishHandlers := NewPublishHandlers(logger, publishmqEventHandler)

	// Admin router is a router group with the API key auth mechanism.
	adminRouter := apiRouter.Group("/", APIKeyAuthMiddleware(cfg.APIKey))

	adminRouter.PUT("/:tenantID", tenantHandlers.Upsert)
	adminRouter.GET("/:tenantID/portal", RequireTenantMiddleware(logger, entityStore), tenantHandlers.RetrievePortal)

	// Tenant router is a router group that accepts either
	// - a tenant's JWT token OR
	// - the preconfigured API key
	//
	// If the EventKit service deployment isn't configured with an API key, then
	// it's assumed that the service runs in a secure environment
	// and the JWT check is NOT necessary either.
	tenantRouter := apiRouter.Group("/",
		APIKeyOrTenantJWTAuthMiddleware(cfg.APIKey, cfg.JWTSecret),
		RequireTenantMiddleware(logger, entityStore),
	)

	tenantRouter.GET("/:tenantID", tenantHandlers.Retrieve)
	tenantRouter.DELETE("/:tenantID", tenantHandlers.Delete)

	tenantRouter.GET("/:tenantID/destinations", destinationHandlers.List)
	tenantRouter.POST("/:tenantID/destinations", destinationHandlers.Create)
	tenantRouter.GET("/:tenantID/destinations/:destinationID", destinationHandlers.Retrieve)
	tenantRouter.PATCH("/:tenantID/destinations/:destinationID", destinationHandlers.Update)
	tenantRouter.DELETE("/:tenantID/destinations/:destinationID", destinationHandlers.Delete)

	adminRouter.POST("/publish", publishHandlers.Ingest)

	return r
}
