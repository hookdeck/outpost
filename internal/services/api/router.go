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

type routeDefinition struct {
	method   string
	path     string
	handlers []gin.HandlerFunc
}

// registerRoutes registers routes to the given router
func registerRoutes(router *gin.RouterGroup, routes []routeDefinition) {
	for _, route := range routes {
		router.Handle(route.method, route.path, route.handlers...)
	}
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
	adminRouter := apiRouter.Group("/",
		APIKeyAuthMiddleware(cfg.APIKey),
	)

	adminRouter.POST("/publish", publishHandlers.Ingest)
	adminRouter.PUT("/:tenantID", SetTenantIDMiddleware(), tenantHandlers.Upsert)

	// Only register token/portal routes when both apiKey and jwtSecret are set
	if cfg.APIKey != "" && cfg.JWTSecret != "" {
		adminRouter.GET("/:tenantID/token", SetTenantIDMiddleware(), RequireTenantMiddleware(logger, entityStore), tenantHandlers.RetrieveToken)
		adminRouter.GET("/:tenantID/portal", SetTenantIDMiddleware(), RequireTenantMiddleware(logger, entityStore), tenantHandlers.RetrievePortal)
	}

	// Generic routes
	// 1: If tenantID param is present, support both API key and JWT auth
	// 2: If tenantID param is not present, also support both API key and JWT auth
	tenantAgnosticRoutes := []routeDefinition{
		{http.MethodGet, "/destination-types", []gin.HandlerFunc{destinationHandlers.ListProviderMetadata}},
		{http.MethodGet, "/destination-types/:type", []gin.HandlerFunc{destinationHandlers.RetrieveProviderMetadata}},
		{http.MethodGet, "/topics", []gin.HandlerFunc{topicHandlers.List}},
	}

	// Tenant-specific routes
	// 1: If tenantID param is present, support both API key and JWT auth
	// 2: If tenantID param is not present, support only JWT auth
	tenantSpecificRoutes := []routeDefinition{
		// Tenant routes
		{http.MethodGet, "", []gin.HandlerFunc{tenantHandlers.Retrieve}},
		{http.MethodDelete, "", []gin.HandlerFunc{tenantHandlers.Delete}},

		// Destination routes
		{http.MethodGet, "/destinations", []gin.HandlerFunc{destinationHandlers.List}},
		{http.MethodPost, "/destinations", []gin.HandlerFunc{destinationHandlers.Create}},
		{http.MethodGet, "/destinations/:destinationID", []gin.HandlerFunc{destinationHandlers.Retrieve}},
		{http.MethodPatch, "/destinations/:destinationID", []gin.HandlerFunc{destinationHandlers.Update}},
		{http.MethodDelete, "/destinations/:destinationID", []gin.HandlerFunc{destinationHandlers.Delete}},
		{http.MethodPut, "/destinations/:destinationID/enable", []gin.HandlerFunc{destinationHandlers.Enable}},
		{http.MethodPut, "/destinations/:destinationID/disable", []gin.HandlerFunc{destinationHandlers.Disable}},

		// Event routes
		{http.MethodGet, "/events", []gin.HandlerFunc{logHandlers.ListEvent}},
		{http.MethodGet, "/events/:eventID", []gin.HandlerFunc{logHandlers.RetrieveEvent}},
		{http.MethodGet, "/events/:eventID/deliveries", []gin.HandlerFunc{logHandlers.ListDeliveryByEvent}},
	}

	// Tenant router with either API key or JWT auth
	tenantParamRouter := apiRouter.Group("/:tenantID",
		SetTenantIDMiddleware(),
		APIKeyOrTenantJWTAuthMiddleware(cfg.APIKey, cfg.JWTSecret),
	)

	// Router without tenantID & JWT auth
	tenantSpecificRouterWithoutTenantID := apiRouter.Group("",
		TenantJWTAuthMiddleware(cfg.APIKey, cfg.JWTSecret),
		RequireTenantMiddleware(logger, entityStore),
	)

	// Router without tenantID params with both API key and JWT auth
	tenantAgnosticRouterWithAuth := apiRouter.Group("",
		APIKeyOrTenantJWTAuthMiddleware(cfg.APIKey, cfg.JWTSecret),
	)

	// Register routes to both routers
	registerRoutes(tenantParamRouter, tenantAgnosticRoutes)
	registerRoutes(tenantParamRouter.Group("", RequireTenantMiddleware(logger, entityStore)), tenantSpecificRoutes)
	registerRoutes(tenantAgnosticRouterWithAuth, tenantAgnosticRoutes)
	registerRoutes(tenantSpecificRouterWithoutTenantID, tenantSpecificRoutes)

	return r
}
