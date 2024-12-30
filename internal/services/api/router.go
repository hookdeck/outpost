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

type TenantMode string

const (
	// No tenant context needed (e.g. /healthz)
	TenantModeNone TenantMode = "none"

	// Tenant context required, can be from:
	// - URL param (:tenantID)
	// - JWT token
	TenantModeRequired TenantMode = "required"

	// Tenant context optional, can be from:
	// - URL param (:tenantID) if present
	// - JWT token if present
	// - No tenant context if using API key
	TenantModeOptional TenantMode = "optional"
)

type RouteDefinition struct {
	Method      string
	Path        string
	Handler     gin.HandlerFunc
	AuthScope   AuthScope
	Mode        RouteMode
	TenantMode  TenantMode
	Middlewares []gin.HandlerFunc
}

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

// registerRoutes registers routes to the given router based on route definitions and config
func registerRoutes(router *gin.RouterGroup, cfg RouterConfig, routes []RouteDefinition, logger *otelzap.Logger, entityStore models.EntityStore) {
	isPortalMode := cfg.APIKey != "" && cfg.JWTSecret != ""

	for _, route := range routes {
		// Skip portal routes if not in portal mode
		if route.Mode == RouteModePortal && !isPortalMode {
			continue
		}

		var handlers []gin.HandlerFunc
		switch route.TenantMode {
		case TenantModeNone:
			// Register route as is
			handlers = buildMiddlewareChain(cfg, route)
			router.Handle(route.Method, route.Path, handlers...)

		case TenantModeRequired:
			// Register with :tenantID prefix (as defined)
			handlers = buildMiddlewareChain(cfg, route)
			router.Handle(route.Method, route.Path, handlers...)

			// For non-admin routes, also register without :tenantID prefix
			if route.AuthScope != AuthScopeAdmin {
				withoutParam := route
				withoutParam.Path = strings.TrimPrefix(route.Path, "/:tenantID")
				handlers = buildMiddlewareChain(cfg, withoutParam)
				router.Handle(withoutParam.Method, withoutParam.Path, handlers...)
			}

		case TenantModeOptional:
			// Register with :tenantID prefix (as defined)
			handlers = buildMiddlewareChain(cfg, route)
			router.Handle(route.Method, route.Path, handlers...)

			// Also register without :tenantID prefix
			withoutParam := route
			withoutParam.Path = strings.TrimPrefix(route.Path, "/:tenantID")
			handlers = buildMiddlewareChain(cfg, withoutParam)
			router.Handle(withoutParam.Method, withoutParam.Path, handlers...)
		}
	}
}

func buildMiddlewareChain(cfg RouterConfig, def RouteDefinition) []gin.HandlerFunc {
	chain := make([]gin.HandlerFunc, 0)

	// For TenantModeRequired without :tenantID in path, force AuthScopeTenant
	// because there's no way to get tenant context without JWT
	authScope := def.AuthScope
	if def.TenantMode == TenantModeRequired && !strings.Contains(def.Path, ":tenantID") {
		authScope = AuthScopeTenant
	}

	// Add auth middleware based on scope
	switch authScope {
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
	apiRouter.Use(SetTenantIDMiddleware())

	apiRouter.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	tenantHandlers := NewTenantHandlers(logger, cfg.JWTSecret, entityStore)
	destinationHandlers := NewDestinationHandlers(logger, entityStore, cfg.Topics, cfg.Registry)
	publishHandlers := NewPublishHandlers(logger, publishmqEventHandler)
	logHandlers := NewLogHandlers(logger, logStore)
	topicHandlers := NewTopicHandlers(logger, cfg.Topics)

	// Admin routes
	adminRoutes := []RouteDefinition{
		{
			Method:     http.MethodPost,
			Path:       "/publish",
			Handler:    publishHandlers.Ingest,
			AuthScope:  AuthScopeAdmin,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeNone,
		},
		{
			Method:     http.MethodPut,
			Path:       "/:tenantID",
			Handler:    tenantHandlers.Upsert,
			AuthScope:  AuthScopeAdmin,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeNone,
		},
	}

	// Portal routes
	portalRoutes := []RouteDefinition{
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID/token",
			Handler:    tenantHandlers.RetrieveToken,
			AuthScope:  AuthScopeAdmin,
			Mode:       RouteModePortal,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID/portal",
			Handler:    tenantHandlers.RetrievePortal,
			AuthScope:  AuthScopeAdmin,
			Mode:       RouteModePortal,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
	}

	// Routes that work with both auth methods
	tenantAgnosticRoutes := []RouteDefinition{
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID/destination-types",
			Handler:    destinationHandlers.ListProviderMetadata,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeOptional,
		},
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID/destination-types/:type",
			Handler:    destinationHandlers.RetrieveProviderMetadata,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeOptional,
		},
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID/topics",
			Handler:    topicHandlers.List,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeOptional,
		},
	}

	// Routes that require tenant context
	tenantSpecificRoutes := []RouteDefinition{
		// Tenant routes
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID",
			Handler:    tenantHandlers.Retrieve,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
		{
			Method:     http.MethodDelete,
			Path:       "/:tenantID",
			Handler:    tenantHandlers.Delete,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},

		// Destination routes
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID/destinations",
			Handler:    destinationHandlers.List,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
		{
			Method:     http.MethodPost,
			Path:       "/:tenantID/destinations",
			Handler:    destinationHandlers.Create,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID/destinations/:destinationID",
			Handler:    destinationHandlers.Retrieve,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
		{
			Method:     http.MethodPatch,
			Path:       "/:tenantID/destinations/:destinationID",
			Handler:    destinationHandlers.Update,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
		{
			Method:     http.MethodDelete,
			Path:       "/:tenantID/destinations/:destinationID",
			Handler:    destinationHandlers.Delete,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
		{
			Method:     http.MethodPut,
			Path:       "/:tenantID/destinations/:destinationID/enable",
			Handler:    destinationHandlers.Enable,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
		{
			Method:     http.MethodPut,
			Path:       "/:tenantID/destinations/:destinationID/disable",
			Handler:    destinationHandlers.Disable,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},

		// Event routes
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID/events",
			Handler:    logHandlers.ListEvent,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID/events/:eventID",
			Handler:    logHandlers.RetrieveEvent,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
		{
			Method:     http.MethodGet,
			Path:       "/:tenantID/events/:eventID/deliveries",
			Handler:    logHandlers.ListDeliveryByEvent,
			AuthScope:  AuthScopeAdminOrTenant,
			Mode:       RouteModeAlways,
			TenantMode: TenantModeRequired,
			Middlewares: []gin.HandlerFunc{
				RequireTenantMiddleware(logger, entityStore),
			},
		},
	}

	// Register all routes to a single router
	apiRoutes := []RouteDefinition{} // combine all routes
	apiRoutes = append(apiRoutes, adminRoutes...)
	apiRoutes = append(apiRoutes, portalRoutes...)
	apiRoutes = append(apiRoutes, tenantAgnosticRoutes...)
	apiRoutes = append(apiRoutes, tenantSpecificRoutes...)

	registerRoutes(apiRouter, cfg, apiRoutes, logger, entityStore)

	return r
}
