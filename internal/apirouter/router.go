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

type AuthScope string

const (
	AuthScopeAdmin         AuthScope = "admin"
	AuthScopeTenant        AuthScope = "tenant"
	AuthScopeAdminOrTenant AuthScope = "admin_or_tenant"
)

type RouteMode string

const (
	RouteModeAlways RouteMode = "always" // Register route regardless of mode
	RouteModePortal RouteMode = "portal" // Only register when portal is enabled (both apiKey and jwtSecret set)
)

type RouteDefinition struct {
	Method      string
	Path        string
	Handler     gin.HandlerFunc
	AuthScope   AuthScope
	Mode        RouteMode
	Middlewares []gin.HandlerFunc
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

// registerRoutes registers routes to the given router based on route definitions and config
func registerRoutes(router *gin.RouterGroup, cfg RouterConfig, routes []RouteDefinition) {
	isPortalMode := cfg.APIKey != "" && cfg.JWTSecret != ""

	for _, route := range routes {
		// Skip portal routes if not in portal mode
		if route.Mode == RouteModePortal && !isPortalMode {
			continue
		}

		handlers := buildMiddlewareChain(cfg, route)
		router.Handle(route.Method, route.Path, handlers...)
	}
}

func buildMiddlewareChain(cfg RouterConfig, def RouteDefinition) []gin.HandlerFunc {
	chain := make([]gin.HandlerFunc, 0)

	// Add auth middleware based on scope
	switch def.AuthScope {
	case AuthScopeAdmin:
		chain = append(chain, APIKeyAuthMiddleware(cfg.APIKey))
	case AuthScopeTenant:
		chain = append(chain, TenantJWTAuthMiddleware(cfg.APIKey, cfg.JWTSecret))
	case AuthScopeAdminOrTenant:
		chain = append(chain, APIKeyOrTenantJWTAuthMiddleware(cfg.APIKey, cfg.JWTSecret))
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
		{
			Method:    http.MethodPost,
			Path:      "/publish",
			Handler:   publishHandlers.Ingest,
			AuthScope: AuthScopeAdmin,
			Mode:      RouteModeAlways,
		},
		{
			Method:    http.MethodGet,
			Path:      "/tenants",
			Handler:   tenantHandlers.List,
			AuthScope: AuthScopeAdmin,
			Mode:      RouteModeAlways,
		},
		{
			Method:    http.MethodGet,
			Path:      "/events",
			Handler:   logHandlers.AdminListEvents,
			AuthScope: AuthScopeAdmin,
			Mode:      RouteModeAlways,
		},
		{
			Method:    http.MethodGet,
			Path:      "/attempts",
			Handler:   logHandlers.AdminListAttempts,
			AuthScope: AuthScopeAdmin,
			Mode:      RouteModeAlways,
		},
	}

	// Tenant upsert route (admin-only, but has :tenantID in path)
	tenantUpsertRoute := RouteDefinition{
		Method:    http.MethodPut,
		Path:      "/:tenantID",
		Handler:   tenantHandlers.Upsert,
		AuthScope: AuthScopeAdmin,
		Mode:      RouteModeAlways,
	}

	// Portal routes
	portalRoutes := []RouteDefinition{
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/token",
			Handler:   tenantHandlers.RetrieveToken,
			AuthScope: AuthScopeAdmin,
			Mode:      RouteModePortal,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/portal",
			Handler:   tenantHandlers.RetrievePortal,
			AuthScope: AuthScopeAdmin,
			Mode:      RouteModePortal,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
	}

	// Routes that work with both auth methods
	tenantAgnosticRoutes := []RouteDefinition{
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/destination-types",
			Handler:   destinationHandlers.ListProviderMetadata,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
		},
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/destination-types/:type",
			Handler:   destinationHandlers.RetrieveProviderMetadata,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
		},
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/topics",
			Handler:   topicHandlers.List,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
		},
	}

	// Routes that require tenant context
	tenantSpecificRoutes := []RouteDefinition{
		// Tenant routes
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID",
			Handler:   tenantHandlers.Retrieve,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodDelete,
			Path:      "/:tenantID",
			Handler:   tenantHandlers.Delete,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},

		// Destination routes
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/destinations",
			Handler:   destinationHandlers.List,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodPost,
			Path:      "/:tenantID/destinations",
			Handler:   destinationHandlers.Create,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/destinations/:destinationID",
			Handler:   destinationHandlers.Retrieve,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodPatch,
			Path:      "/:tenantID/destinations/:destinationID",
			Handler:   destinationHandlers.Update,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodDelete,
			Path:      "/:tenantID/destinations/:destinationID",
			Handler:   destinationHandlers.Delete,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodPut,
			Path:      "/:tenantID/destinations/:destinationID/enable",
			Handler:   destinationHandlers.Enable,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodPut,
			Path:      "/:tenantID/destinations/:destinationID/disable",
			Handler:   destinationHandlers.Disable,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},

		// Destination-scoped attempt routes
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/destinations/:destinationID/attempts",
			Handler:   logHandlers.ListDestinationAttempts,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/destinations/:destinationID/attempts/:attemptID",
			Handler:   logHandlers.RetrieveAttempt,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodPost,
			Path:      "/:tenantID/destinations/:destinationID/attempts/:attemptID/retry",
			Handler:   retryHandlers.RetryAttempt,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},

		// Event routes
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/events",
			Handler:   logHandlers.ListEvents,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/events/:eventID",
			Handler:   logHandlers.RetrieveEvent,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},

		// Attempt routes
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/attempts",
			Handler:   logHandlers.ListAttempts,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodGet,
			Path:      "/:tenantID/attempts/:attemptID",
			Handler:   logHandlers.RetrieveAttempt,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
		{
			Method:    http.MethodPost,
			Path:      "/:tenantID/attempts/:attemptID/retry",
			Handler:   retryHandlers.RetryAttempt,
			AuthScope: AuthScopeAdminOrTenant,
			Mode:      RouteModeAlways,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(tenantStore),
			},
		},
	}

	// Register non-tenant routes at root
	registerRoutes(apiRouter, cfg, nonTenantRoutes)

	// Combine all tenant-scoped routes (routes with :tenantID in path)
	tenantScopedRoutes := []RouteDefinition{}
	tenantScopedRoutes = append(tenantScopedRoutes, tenantUpsertRoute)
	tenantScopedRoutes = append(tenantScopedRoutes, portalRoutes...)
	tenantScopedRoutes = append(tenantScopedRoutes, tenantAgnosticRoutes...)
	tenantScopedRoutes = append(tenantScopedRoutes, tenantSpecificRoutes...)

	// Register tenant-scoped routes under /tenants prefix
	tenantsGroup := apiRouter.Group("/tenants")
	registerRoutes(tenantsGroup, cfg, tenantScopedRoutes)

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
