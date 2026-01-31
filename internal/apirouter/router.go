package apirouter

import (
	"errors"
	"net/http"
	"reflect"
	"strings"

	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/portal"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

type RouteDefinition struct {
	Method        string
	Path          string
	Handler       gin.HandlerFunc
	AdminOnly     bool
	RequireTenant bool
	Middlewares   []gin.HandlerFunc
}

type RouterConfig struct {
	ServiceName  string
	APIKey       string
	JWTSecret    string
	DeploymentID string
	Topics       []string
	Registry     destregistry.Registry
	PortalConfig portal.PortalConfig
	GinMode      string
}

type RouterDeps struct {
	TenantStore       tenantstore.TenantStore
	LogStore          logstore.LogStore
	Logger            *logging.Logger
	DeliveryPublisher deliveryPublisher
	EventHandler      eventHandler
	Telemetry         telemetry.Telemetry
}

func (d RouterDeps) validate() error {
	if d.TenantStore == nil {
		return fmt.Errorf("apirouter: TenantStore is required")
	}
	if d.LogStore == nil {
		return fmt.Errorf("apirouter: LogStore is required")
	}
	if d.Logger == nil {
		return fmt.Errorf("apirouter: Logger is required")
	}
	if d.DeliveryPublisher == nil {
		return fmt.Errorf("apirouter: DeliveryPublisher is required")
	}
	if d.EventHandler == nil {
		return fmt.Errorf("apirouter: EventHandler is required")
	}
	if d.Telemetry == nil {
		return fmt.Errorf("apirouter: Telemetry is required")
	}
	return nil
}

// registerRoutes registers routes to the given router based on route definitions and config
func registerRoutes(router *gin.RouterGroup, cfg RouterConfig, tenantRetriever TenantRetriever, routes []RouteDefinition) {
	for _, route := range routes {
		handlers := buildMiddlewareChain(cfg, tenantRetriever, route)
		router.Handle(route.Method, route.Path, handlers...)
	}
}

func buildMiddlewareChain(cfg RouterConfig, tenantRetriever TenantRetriever, def RouteDefinition) []gin.HandlerFunc {
	chain := make([]gin.HandlerFunc, 0)

	chain = append(chain, AuthMiddleware(cfg.APIKey, cfg.JWTSecret, tenantRetriever, AuthOptions{
		AdminOnly:     def.AdminOnly,
		RequireTenant: def.RequireTenant,
	}))

	// Add custom middlewares
	chain = append(chain, def.Middlewares...)

	// Add the main handler
	chain = append(chain, def.Handler)

	return chain
}

func NewRouter(cfg RouterConfig, deps RouterDeps) http.Handler {
	if err := deps.validate(); err != nil {
		panic(err)
	}

	// Only set mode from config if we're not in test mode
	if gin.Mode() != gin.TestMode {
		gin.SetMode(cfg.GinMode)
	}

	r := gin.New()
	// Core middlewares
	r.Use(gin.Recovery())
	r.Use(deps.Telemetry.MakeSentryHandler())
	r.Use(otelgin.Middleware(cfg.ServiceName))
	r.Use(MetricsMiddleware())

	// Create sanitizer for secure request body logging on 5xx errors
	sanitizer := NewRequestBodySanitizer(cfg.Registry)
	r.Use(LoggerMiddlewareWithSanitizer(deps.Logger, sanitizer))

	r.Use(LatencyMiddleware()) // LatencyMiddleware must be after Metrics & Logger to fully capture latency first

	// Application logic
	r.Use(ErrorHandlerMiddleware())

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})
	}

	portal.AddRoutes(r, cfg.PortalConfig)

	apiRouter := r.Group("/api/v1")

	tenantHandlers := NewTenantHandlers(deps.Logger, deps.Telemetry, cfg.JWTSecret, cfg.DeploymentID, deps.TenantStore)
	destinationHandlers := NewDestinationHandlers(deps.Logger, deps.Telemetry, deps.TenantStore, cfg.Topics, cfg.Registry)
	publishHandlers := NewPublishHandlers(deps.Logger, deps.EventHandler)
	logHandlers := NewLogHandlers(deps.Logger, deps.LogStore)
	retryHandlers := NewRetryHandlers(deps.Logger, deps.TenantStore, deps.LogStore, deps.DeliveryPublisher)
	topicHandlers := NewTopicHandlers(deps.Logger, cfg.Topics)

	routes := []RouteDefinition{
		// Schemas & Topics
		{Method: http.MethodGet, Path: "/destination-types", Handler: destinationHandlers.ListProviderMetadata},
		{Method: http.MethodGet, Path: "/destination-types/:type", Handler: destinationHandlers.RetrieveProviderMetadata},
		{Method: http.MethodGet, Path: "/topics", Handler: topicHandlers.List},

		// Publish / Retry
		{Method: http.MethodPost, Path: "/publish", Handler: publishHandlers.Ingest, AdminOnly: true},
		{Method: http.MethodPost, Path: "/retry", Handler: retryHandlers.Retry},

		// Tenants
		{Method: http.MethodGet, Path: "/tenants", Handler: tenantHandlers.List},
		{Method: http.MethodPut, Path: "/tenants/:tenantID", Handler: tenantHandlers.Upsert},
		{Method: http.MethodGet, Path: "/tenants/:tenantID", Handler: tenantHandlers.Retrieve, RequireTenant: true},
		{Method: http.MethodDelete, Path: "/tenants/:tenantID", Handler: tenantHandlers.Delete, RequireTenant: true},
		{Method: http.MethodGet, Path: "/tenants/:tenantID/token", Handler: tenantHandlers.RetrieveToken, AdminOnly: true, RequireTenant: true},
		{Method: http.MethodGet, Path: "/tenants/:tenantID/portal", Handler: tenantHandlers.RetrievePortal, AdminOnly: true, RequireTenant: true},

		// Destinations
		{Method: http.MethodGet, Path: "/tenants/:tenantID/destinations", Handler: destinationHandlers.List, RequireTenant: true},
		{Method: http.MethodPost, Path: "/tenants/:tenantID/destinations", Handler: destinationHandlers.Create, RequireTenant: true},
		{Method: http.MethodGet, Path: "/tenants/:tenantID/destinations/:destinationID", Handler: destinationHandlers.Retrieve, RequireTenant: true},
		{Method: http.MethodPatch, Path: "/tenants/:tenantID/destinations/:destinationID", Handler: destinationHandlers.Update, RequireTenant: true},
		{Method: http.MethodDelete, Path: "/tenants/:tenantID/destinations/:destinationID", Handler: destinationHandlers.Delete, RequireTenant: true},
		{Method: http.MethodPut, Path: "/tenants/:tenantID/destinations/:destinationID/enable", Handler: destinationHandlers.Enable, RequireTenant: true},
		{Method: http.MethodPut, Path: "/tenants/:tenantID/destinations/:destinationID/disable", Handler: destinationHandlers.Disable, RequireTenant: true},
		{Method: http.MethodGet, Path: "/tenants/:tenantID/destinations/:destinationID/attempts", Handler: logHandlers.ListDestinationAttempts, RequireTenant: true},
		{Method: http.MethodGet, Path: "/tenants/:tenantID/destinations/:destinationID/attempts/:attemptID", Handler: logHandlers.RetrieveAttempt, RequireTenant: true},

		// Events
		{Method: http.MethodGet, Path: "/events", Handler: logHandlers.ListEvents},
		{Method: http.MethodGet, Path: "/events/:eventID", Handler: logHandlers.RetrieveEvent},

		// Attempts
		{Method: http.MethodGet, Path: "/attempts", Handler: logHandlers.ListAttempts},
		{Method: http.MethodGet, Path: "/attempts/:attemptID", Handler: logHandlers.RetrieveAttempt},
	}

	registerRoutes(apiRouter, cfg, deps.TenantStore, routes)

	// Register dev routes
	if gin.Mode() == gin.DebugMode {
		registerDevRoutes(apiRouter)
	}

	return r
}

func registerDevRoutes(apiRouter *gin.RouterGroup) {
	apiRouter.GET("/dev/err/panic", func(c *gin.Context) {
		panic("test panic error")
	})

	apiRouter.GET("/dev/err/internal", func(c *gin.Context) {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(errors.New("test internal error")))
	})
}
