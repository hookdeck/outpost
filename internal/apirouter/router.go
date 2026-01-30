package apirouter

import (
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/portal"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

type AuthMode string

const (
	AuthPublic AuthMode = "public"
	AuthTenant AuthMode = "tenant"
	AuthAdmin  AuthMode = "admin"
)

type RouteDefinition struct {
	Method       string
	Path         string
	Handler      gin.HandlerFunc
	AuthMode     AuthMode
	TenantScoped bool
	Middlewares  []gin.HandlerFunc
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

func (c RouterConfig) PortalEnabled() bool {
	return c.APIKey != "" && c.JWTSecret != ""
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

	// Add auth middleware based on mode
	switch def.AuthMode {
	case AuthAdmin:
		chain = append(chain, APIKeyAuthMiddleware(cfg.APIKey))
	case AuthTenant:
		chain = append(chain, APIKeyOrTenantJWTAuthMiddleware(cfg.APIKey, cfg.JWTSecret))
	case AuthPublic:
		// no auth middleware
	}

	// Auto-apply tenant middleware when route is tenant-scoped
	if def.TenantScoped {
		chain = append(chain, RequireTenantMiddleware(tenantRetriever))
	}

	// Add custom middlewares
	chain = append(chain, def.Middlewares...)

	// Add the main handler
	chain = append(chain, def.Handler)

	return chain
}

func NewRouter(
	cfg RouterConfig,
	logger *logging.Logger,
	redisClient redis.Cmdable,
	deliveryMQ *deliverymq.DeliveryMQ,
	tenantStore tenantstore.TenantStore,
	logStore logstore.LogStore,
	publishmqEventHandler publishmq.EventHandler,
	telemetry telemetry.Telemetry,
) http.Handler {
	// Only set mode from config if we're not in test mode
	if gin.Mode() != gin.TestMode {
		gin.SetMode(cfg.GinMode)
	}

	r := gin.New()
	// Core middlewares
	r.Use(gin.Recovery())
	r.Use(telemetry.MakeSentryHandler())
	r.Use(otelgin.Middleware(cfg.ServiceName))
	r.Use(MetricsMiddleware())

	// Create sanitizer for secure request body logging on 5xx errors
	sanitizer := NewRequestBodySanitizer(cfg.Registry)
	r.Use(LoggerMiddlewareWithSanitizer(logger, sanitizer))

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
	apiRouter.Use(SetTenantIDMiddleware())

	tenantHandlers := NewTenantHandlers(logger, telemetry, cfg.JWTSecret, cfg.DeploymentID, tenantStore)
	destinationHandlers := NewDestinationHandlers(logger, telemetry, tenantStore, cfg.Topics, cfg.Registry)
	publishHandlers := NewPublishHandlers(logger, publishmqEventHandler)
	logHandlers := NewLogHandlers(logger, logStore)
	retryHandlers := NewRetryHandlers(logger, tenantStore, logStore, deliveryMQ)
	topicHandlers := NewTopicHandlers(logger, cfg.Topics)

	// Non-tenant routes (no :tenantID in path)
	nonTenantRoutes := []RouteDefinition{
		{Method: http.MethodPost, Path: "/publish", Handler: publishHandlers.Ingest, AuthMode: AuthAdmin},
		{Method: http.MethodGet, Path: "/tenants", Handler: tenantHandlers.List, AuthMode: AuthAdmin},
		{Method: http.MethodGet, Path: "/events", Handler: logHandlers.AdminListEvents, AuthMode: AuthAdmin},
		{Method: http.MethodGet, Path: "/attempts", Handler: logHandlers.AdminListAttempts, AuthMode: AuthAdmin},
	}

	// Tenant routes (registered under /tenants group)
	tenantRoutes := []RouteDefinition{
		// Tenant CRUD
		{Method: http.MethodPut, Path: "/:tenantID", Handler: tenantHandlers.Upsert, AuthMode: AuthAdmin},
		{Method: http.MethodGet, Path: "/:tenantID", Handler: tenantHandlers.Retrieve, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodDelete, Path: "/:tenantID", Handler: tenantHandlers.Delete, AuthMode: AuthTenant, TenantScoped: true},

		// Tenant-agnostic routes (no tenant lookup needed)
		{Method: http.MethodGet, Path: "/:tenantID/destination-types", Handler: destinationHandlers.ListProviderMetadata, AuthMode: AuthTenant},
		{Method: http.MethodGet, Path: "/:tenantID/destination-types/:type", Handler: destinationHandlers.RetrieveProviderMetadata, AuthMode: AuthTenant},
		{Method: http.MethodGet, Path: "/:tenantID/topics", Handler: topicHandlers.List, AuthMode: AuthTenant},

		// Destination routes
		{Method: http.MethodGet, Path: "/:tenantID/destinations", Handler: destinationHandlers.List, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodPost, Path: "/:tenantID/destinations", Handler: destinationHandlers.Create, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodGet, Path: "/:tenantID/destinations/:destinationID", Handler: destinationHandlers.Retrieve, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodPatch, Path: "/:tenantID/destinations/:destinationID", Handler: destinationHandlers.Update, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodDelete, Path: "/:tenantID/destinations/:destinationID", Handler: destinationHandlers.Delete, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodPut, Path: "/:tenantID/destinations/:destinationID/enable", Handler: destinationHandlers.Enable, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodPut, Path: "/:tenantID/destinations/:destinationID/disable", Handler: destinationHandlers.Disable, AuthMode: AuthTenant, TenantScoped: true},

		// Destination-scoped attempt routes
		{Method: http.MethodGet, Path: "/:tenantID/destinations/:destinationID/attempts", Handler: logHandlers.ListDestinationAttempts, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodGet, Path: "/:tenantID/destinations/:destinationID/attempts/:attemptID", Handler: logHandlers.RetrieveAttempt, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodPost, Path: "/:tenantID/destinations/:destinationID/attempts/:attemptID/retry", Handler: retryHandlers.RetryAttempt, AuthMode: AuthTenant, TenantScoped: true},

		// Event routes
		{Method: http.MethodGet, Path: "/:tenantID/events", Handler: logHandlers.ListEvents, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodGet, Path: "/:tenantID/events/:eventID", Handler: logHandlers.RetrieveEvent, AuthMode: AuthTenant, TenantScoped: true},

		// Attempt routes
		{Method: http.MethodGet, Path: "/:tenantID/attempts", Handler: logHandlers.ListAttempts, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodGet, Path: "/:tenantID/attempts/:attemptID", Handler: logHandlers.RetrieveAttempt, AuthMode: AuthTenant, TenantScoped: true},
		{Method: http.MethodPost, Path: "/:tenantID/attempts/:attemptID/retry", Handler: retryHandlers.RetryAttempt, AuthMode: AuthTenant, TenantScoped: true},
	}

	// Portal routes (conditionally appended)
	portalRoutes := []RouteDefinition{
		{Method: http.MethodGet, Path: "/:tenantID/token", Handler: tenantHandlers.RetrieveToken, AuthMode: AuthAdmin, TenantScoped: true},
		{Method: http.MethodGet, Path: "/:tenantID/portal", Handler: tenantHandlers.RetrievePortal, AuthMode: AuthAdmin, TenantScoped: true},
	}

	if cfg.PortalEnabled() {
		tenantRoutes = append(tenantRoutes, portalRoutes...)
	}

	// Register non-tenant routes at root
	registerRoutes(apiRouter, cfg, tenantStore, nonTenantRoutes)

	// Register tenant routes under /tenants prefix
	tenantsGroup := apiRouter.Group("/tenants")
	registerRoutes(tenantsGroup, cfg, tenantStore, tenantRoutes)

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
