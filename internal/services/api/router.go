package api

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/portal"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

type RouterConfig struct {
	Hostname       string
	APIKey         string
	JWTSecret      string
	PortalProxyURL string
	Topics         []string
	Registry       destregistry.Registry
}

func NewRouter(
	cfg RouterConfig,
	portalConfigs map[string]string,
	logger *otelzap.Logger,
	redisClient *redis.Client,
	deliveryMQ *deliverymq.DeliveryMQ,
	entityStore models.EntityStore,
	logStore models.LogStore,
	publishmqEventHandler publishmq.EventHandler,
) http.Handler {
	r := gin.Default()

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})
	}

	r.Use(otelgin.Middleware(cfg.Hostname))
	r.Use(MetricsMiddleware())
	r.Use(ErrorHandlerMiddleware(logger))

	portal.AddRoutes(r, portal.PortalConfig{
		ProxyURL: cfg.PortalProxyURL,
		Configs:  portalConfigs,
	})

	apiRouter := r.Group("/api/v1")

	apiRouter.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	tenantHandlers := NewTenantHandlers(logger, cfg.JWTSecret, entityStore)
	destinationHandlers := NewDestinationHandlers(logger, entityStore, cfg.Topics, cfg.Registry)
	publishHandlers := NewPublishHandlers(logger, publishmqEventHandler)
	logHandlers := NewLogHandlers(logger, logStore)
	topicHandlers := NewTopicHandlers(logger, cfg.Topics)

	// Admin router is a router group with the API key auth mechanism.
	adminRouter := apiRouter.Group("/", APIKeyAuthMiddleware(cfg.APIKey))

	adminRouter.PUT("/:tenantID", tenantHandlers.Upsert)
	adminRouter.GET("/:tenantID/portal", RequireTenantMiddleware(logger, entityStore), tenantHandlers.RetrievePortal)

	adminRouter.GET("/destination-types", destinationHandlers.ListProviderMetadata)
	adminRouter.GET("/destination-types/:type", destinationHandlers.RetrieveProviderMetadata)
	adminRouter.GET("/topics", topicHandlers.List)
	adminRouter.POST("/publish", publishHandlers.Ingest)

	// Tenant router is a router group that accepts either
	// - a tenant's JWT token OR
	// - the preconfigured API key
	//
	// If the Outpost service deployment isn't configured with an API key, then
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
	tenantRouter.PUT("/:tenantID/destinations/:destinationID/enable", destinationHandlers.Enable)
	tenantRouter.PUT("/:tenantID/destinations/:destinationID/disable", destinationHandlers.Disable)

	tenantRouter.GET("/:tenantID/events", logHandlers.ListEvent)
	tenantRouter.GET("/:tenantID/events/:eventID", logHandlers.RetrieveEvent)
	tenantRouter.GET("/:tenantID/events/:eventID/deliveries", logHandlers.ListDeliveryByEvent)

	tenantRouter.GET("/:tenantID/destination-types", destinationHandlers.ListProviderMetadata)
	tenantRouter.GET("/:tenantID/destination-types/:type", destinationHandlers.RetrieveProviderMetadata)
	tenantRouter.GET("/:tenantID/topics", topicHandlers.List)

	return r
}
