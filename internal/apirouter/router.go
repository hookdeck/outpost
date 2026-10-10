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
	"github.com/hookdeck/outpost/internal/topicschema"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

type RouteDefinition struct {
	Method        string
	Path          string
	Handler       gin.HandlerFunc
	AdminOnly     bool
	RequireTenant bool
	Middlewares   []gin.HandlerFunc
	// MinVersion is the first API version serving the route. 0 serves it in
	// every version.
	MinVersion apiVersion
}

const publishPath = "/publish"

type RouterConfig struct {
	ServiceName          string
	APIKey               string
	JWTSecret            string
	DeploymentID         string
	Topics               []string
	TopicsAllowWildcards bool
	// TopicCatalog describes the topics in v2. Nil serves Topics without
	// schemas.
	TopicCatalog *topicschema.Catalog
	Registry     destregistry.Registry
	PortalConfig portal.PortalConfig
	GinMode      string
}

type RouterDeps struct {
	TenantStore         tenantstore.TenantStore
	LogStore            logstore.LogStore
	Logger              *logging.Logger
	DeliveryPublisher   deliveryPublisher
	EventHandler        eventHandler
	Telemetry           telemetry.Telemetry
	SubscriptionEmitter SubscriptionEmitter // optional — emits tenant.subscription.updated on destination mutations
	// MCP holds the dependencies of the MCP Events endpoints. Optional: when
	// nil, those endpoints are still routed in API v2 and answer 503.
	MCP *MCPDeps
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

// registerRoutes registers the routes served in version v to the version's group
func registerRoutes(router *gin.RouterGroup, v apiVersion, cfg RouterConfig, tenantRetriever TenantRetriever, routes []RouteDefinition) {
	for _, route := range routes {
		if route.MinVersion > v {
			continue
		}
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
	r.Use(RecoveryMiddleware())
	r.Use(deps.Telemetry.MakeSentryHandler())
	r.Use(otelgin.Middleware(cfg.ServiceName))
	r.Use(MetricsMiddleware())

	// Create sanitizer for secure request body logging on 5xx errors
	sanitizer := NewRequestBodySanitizer(cfg.Registry)
	r.Use(LoggerMiddlewareWithSanitizer(deps.Logger, sanitizer))

	r.Use(LatencyMiddleware()) // LatencyMiddleware must be after Metrics & Logger to fully capture latency first

	// Application logic
	r.Use(ErrorHandlerMiddleware())
	r.Use(RequestBodyLimitMiddleware())

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})
	}

	portal.AddRoutes(r, cfg.PortalConfig, func(c *gin.Context) {
		AbortWithError(c, http.StatusNotFound, ErrorResponse{Code: http.StatusNotFound, Message: "not found"})
	})

	catalog := cfg.TopicCatalog
	if catalog == nil {
		catalog = topicschema.EmptyCatalog(cfg.Topics)
	}

	displayer := newDestinationDisplayer(cfg.Registry, cfg.TopicsAllowWildcards)

	tenantHandlers := NewTenantHandlers(deps.Logger, deps.Telemetry, cfg.JWTSecret, cfg.DeploymentID, deps.TenantStore, cfg.TopicsAllowWildcards)
	mcpHandlers := NewMCPHandlers(deps.Logger, deps.Telemetry, deps.TenantStore, cfg.Registry, catalog, deps.MCP)
	var mcpServerURL string
	if deps.MCP != nil {
		mcpServerURL = deps.MCP.Config.ServerURL
	}
	destinationTypes, err := newDestinationTypes(cfg.Registry, catalog, mcpServerURL)
	if err != nil {
		panic(fmt.Errorf("apirouter: %w", err))
	}
	destinationHandlers := NewDestinationHandlers(deps.Logger, deps.Telemetry, deps.TenantStore, deps.SubscriptionEmitter, cfg.Topics, cfg.TopicsAllowWildcards, cfg.Registry, displayer)
	destinationHandlers.types = destinationTypes
	destinationHandlers.mcp = mcpHandlers
	publishHandlers := NewPublishHandlers(deps.Logger, deps.EventHandler)
	logHandlers := NewLogHandlers(deps.Logger, deps.LogStore, deps.TenantStore, displayer)
	retryHandlers := NewRetryHandlers(deps.Logger, deps.TenantStore, deps.LogStore, deps.DeliveryPublisher, cfg.TopicsAllowWildcards)
	topicHandlers, err := NewTopicHandlers(deps.Logger, cfg.Topics, catalog)
	if err != nil {
		panic(fmt.Errorf("apirouter: %w", err))
	}
	metricsHandlers := NewMetricsHandlers(deps.Logger, deps.LogStore)
	mcpMiddlewares := []gin.HandlerFunc{mcpHandlers.RequireConfigured}

	routes := []RouteDefinition{
		// Schemas & Topics
		{Method: http.MethodGet, Path: "/destination-types", Handler: destinationHandlers.ListProviderMetadata},
		{Method: http.MethodGet, Path: "/destination-types/:type", Handler: destinationHandlers.RetrieveProviderMetadata},
		{Method: http.MethodGet, Path: "/topics", Handler: topicHandlers.List},

		// Publish / Retry
		{Method: http.MethodPost, Path: publishPath, Handler: publishHandlers.Ingest, AdminOnly: true},
		{Method: http.MethodPost, Path: "/retry", Handler: retryHandlers.Retry},

		// Tenants
		{Method: http.MethodGet, Path: "/tenants", Handler: tenantHandlers.List},
		{Method: http.MethodPut, Path: "/tenants/:tenant_id", Handler: tenantHandlers.Upsert},
		{Method: http.MethodGet, Path: "/tenants/:tenant_id", Handler: tenantHandlers.Retrieve, RequireTenant: true},
		{Method: http.MethodDelete, Path: "/tenants/:tenant_id", Handler: tenantHandlers.Delete, RequireTenant: true},
		{Method: http.MethodGet, Path: "/tenants/:tenant_id/token", Handler: tenantHandlers.RetrieveToken, AdminOnly: true, RequireTenant: true},
		{Method: http.MethodGet, Path: "/tenants/:tenant_id/portal", Handler: tenantHandlers.RetrievePortal, AdminOnly: true, RequireTenant: true},

		// Destinations
		{Method: http.MethodGet, Path: "/tenants/:tenant_id/destinations", Handler: destinationHandlers.List, RequireTenant: true},
		{Method: http.MethodPost, Path: "/tenants/:tenant_id/destinations", Handler: destinationHandlers.Create, RequireTenant: true},
		{Method: http.MethodGet, Path: "/tenants/:tenant_id/destinations/:destination_id", Handler: destinationHandlers.Retrieve, RequireTenant: true},
		{Method: http.MethodPatch, Path: "/tenants/:tenant_id/destinations/:destination_id", Handler: destinationHandlers.Update, RequireTenant: true},
		{Method: http.MethodDelete, Path: "/tenants/:tenant_id/destinations/:destination_id", Handler: destinationHandlers.Delete, RequireTenant: true},
		{Method: http.MethodPut, Path: "/tenants/:tenant_id/destinations/:destination_id/enable", Handler: destinationHandlers.Enable, RequireTenant: true},
		{Method: http.MethodPut, Path: "/tenants/:tenant_id/destinations/:destination_id/disable", Handler: destinationHandlers.Disable, RequireTenant: true},
		{Method: http.MethodGet, Path: "/tenants/:tenant_id/destinations/:destination_id/attempts", Handler: logHandlers.ListDestinationAttempts, RequireTenant: true},
		{Method: http.MethodGet, Path: "/tenants/:tenant_id/destinations/:destination_id/attempts/:attempt_id", Handler: logHandlers.RetrieveAttempt, RequireTenant: true},

		// Events
		{Method: http.MethodGet, Path: "/events", Handler: logHandlers.ListEvents},
		{Method: http.MethodGet, Path: "/events/:event_id", Handler: logHandlers.RetrieveEvent},

		// Attempts
		{Method: http.MethodGet, Path: "/attempts", Handler: logHandlers.ListAttempts},
		{Method: http.MethodGet, Path: "/attempts/:attempt_id", Handler: logHandlers.RetrieveAttempt},

		// Metrics
		{Method: http.MethodGet, Path: "/metrics/events", Handler: metricsHandlers.MetricsEvents},
		{Method: http.MethodGet, Path: "/metrics/attempts", Handler: metricsHandlers.MetricsAttempts},

		// MCP Events. The first three serve an operator's MCP server, which
		// passes its principal: admin only. Subscribe resolves the tenant
		// itself, to answer a missing one with an mcp_error, and unsubscribe
		// and the event list don't need it.
		{Method: http.MethodGet, Path: "/tenants/:tenant_id/mcp/events", Handler: mcpHandlers.ListEvents, AdminOnly: true, Middlewares: mcpMiddlewares, MinVersion: apiV2},
		{Method: http.MethodPut, Path: "/tenants/:tenant_id/mcp/subscriptions", Handler: mcpHandlers.Subscribe, AdminOnly: true, Middlewares: mcpMiddlewares, MinVersion: apiV2},
		{Method: http.MethodPost, Path: "/tenants/:tenant_id/mcp/subscriptions/unsubscribe", Handler: mcpHandlers.Unsubscribe, AdminOnly: true, Middlewares: mcpMiddlewares, MinVersion: apiV2},
		{Method: http.MethodGet, Path: "/tenants/:tenant_id/mcp/subscriptions", Handler: mcpHandlers.ListSubscriptions, RequireTenant: true, Middlewares: mcpMiddlewares, MinVersion: apiV2},
		{Method: http.MethodDelete, Path: "/tenants/:tenant_id/mcp/subscriptions/:subscription_id", Handler: mcpHandlers.DeleteSubscription, RequireTenant: true, Middlewares: mcpMiddlewares, MinVersion: apiV2},
		{Method: http.MethodDelete, Path: "/tenants/:tenant_id/mcp/subscriptions", Handler: mcpHandlers.DeleteSubscriptions, AdminOnly: true, RequireTenant: true, Middlewares: mcpMiddlewares, MinVersion: apiV2},
	}

	for _, v := range apiVersions {
		// The version middleware is given to Group, so every route registered
		// on the group runs it.
		apiRouter := r.Group(v.basePath(), apiVersionMiddleware(v))
		registerRoutes(apiRouter, v, cfg, deps.TenantStore, routes)

		// Register dev routes
		if v == apiV1 && gin.Mode() == gin.DebugMode {
			registerDevRoutes(apiRouter)
		}
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
