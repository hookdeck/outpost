package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/EventKit/internal/models"
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
	tenantModel *models.TenantModel,
	destinationModel *models.DestinationModel,
) http.Handler {
	r := gin.Default()
	r.Use(otelgin.Middleware(cfg.Hostname))

	r.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	tenantHandlers := NewTenantHandlers(logger, tenantModel, cfg.JWTSecret)
	destinationHandlers := NewDestinationHandlers(logger, destinationModel)

	// Admin router is a router group with the API key auth mechanism.
	adminRouter := r.Group("/", APIKeyAuthMiddleware(cfg.APIKey))

	adminRouter.PUT("/:tenantID", tenantHandlers.Upsert)
	adminRouter.GET("/:tenantID/portal", tenantHandlers.RetrievePortal)

	// Tenant router is a router group that accepts either
	// - a tenant's JWT token OR
	// - the preconfigured API key
	//
	// If the EventKit service deployment isn't configured with an API key, then
	// it's assumed that the service runs in a secure environment
	// and the JWT check is NOT necessary either.
	tenantRouter := r.Group("/", APIKeyOrTenantJWTAuthMiddleware(cfg.APIKey, cfg.JWTSecret), requireTenantMiddleware(logger, tenantModel))

	tenantRouter.GET("/:tenantID", tenantHandlers.Retrieve)
	tenantRouter.DELETE("/:tenantID", tenantHandlers.Delete)

	r.GET("/:tenantID/destinations", destinationHandlers.List)
	r.POST("/:tenantID/destinations", destinationHandlers.Create)
	r.GET("/:tenantID/destinations/:destinationID", destinationHandlers.Retrieve)
	r.PATCH("/:tenantID/destinations/:destinationID", destinationHandlers.Update)
	r.DELETE("/:tenantID/destinations/:destinationID", destinationHandlers.Delete)

	return r
}
