package apirouter

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
)

var (
	ErrMissingAuthHeader  = errors.New("missing authorization header")
	ErrInvalidBearerToken = errors.New("invalid bearer token format")
)

const (
	// Context keys
	authRoleKey = "authRole"

	// Role values
	RoleAdmin  = "admin"
	RoleTenant = "tenant"
)

// TenantRetriever is satisfied by tenantstore.TenantStore.
// Defined here to avoid coupling the router/middleware to the full store interface.
type TenantRetriever interface {
	RetrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error)
}

// validateAuthHeader checks the Authorization header and returns the token if valid
func validateAuthHeader(c *gin.Context) (string, error) {
	header := c.GetHeader("Authorization")
	if header == "" {
		return "", ErrMissingAuthHeader
	}
	if !strings.HasPrefix(header, "Bearer ") {
		return "", ErrInvalidBearerToken
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" {
		return "", ErrInvalidBearerToken
	}
	return token, nil
}

func AdminMiddleware(apiKey string) gin.HandlerFunc {
	// When apiKey is empty, everything is admin-only through VPC
	if apiKey == "" {
		return func(c *gin.Context) {
			c.Set(authRoleKey, RoleAdmin)
			c.Next()
		}
	}

	return func(c *gin.Context) {
		token, err := validateAuthHeader(c)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		if token != apiKey {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set(authRoleKey, RoleAdmin)
		c.Next()
	}
}

func AuthenticatedMiddleware(apiKey string, jwtKey string) gin.HandlerFunc {
	// When apiKey is empty, everything is admin-only through VPC
	if apiKey == "" {
		return func(c *gin.Context) {
			c.Set(authRoleKey, RoleAdmin)
			c.Next()
		}
	}

	return func(c *gin.Context) {
		token, err := validateAuthHeader(c)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// Try API key first
		if token == apiKey {
			c.Set(authRoleKey, RoleAdmin)
			c.Next()
			return
		}

		// Try JWT auth
		claims, err := JWT.Extract(jwtKey, token)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// If tenantID param exists, verify it matches token
		if paramTenantID := c.Param("tenantID"); paramTenantID != "" && paramTenantID != claims.TenantID {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Set("tenantID", claims.TenantID)
		c.Set(authRoleKey, RoleTenant)
		c.Next()
	}
}

// resolveTenantMiddleware resolves the tenant from the DB and sets it in context.
// For JWT auth (tenantID already in context), it resolves using that ID.
// For API key auth on tenant-scoped routes, it resolves using the :tenantID URL param.
// When requireTenant is true, missing/deleted tenant returns an error (404 for admin, 401 for JWT).
// When requireTenant is false, it only resolves if JWT set a tenantID in context.
func resolveTenantMiddleware(tenantRetriever TenantRetriever, requireTenant bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, isJWT := c.Get("tenantID")

		if !requireTenant && !isJWT {
			c.Next()
			return
		}

		tenantID := tenantIDFromContext(c)
		if tenantID == "" {
			if requireTenant {
				AbortWithError(c, http.StatusNotFound, NewErrNotFound("tenant"))
			} else {
				c.Next()
			}
			return
		}

		tenant, err := tenantRetriever.RetrieveTenant(c.Request.Context(), tenantID)
		if err != nil {
			if err == tenantstore.ErrTenantDeleted {
				if isJWT {
					c.AbortWithStatus(http.StatusUnauthorized)
				} else {
					AbortWithError(c, http.StatusNotFound, NewErrNotFound("tenant"))
				}
				return
			}
			AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
			return
		}
		if tenant == nil {
			if isJWT {
				c.AbortWithStatus(http.StatusUnauthorized)
			} else {
				AbortWithError(c, http.StatusNotFound, NewErrNotFound("tenant"))
			}
			return
		}

		c.Set("tenant", tenant)
		c.Next()
	}
}

// tenantIDFromContext returns the tenant ID from context (set by JWT middleware) or
// falls back to the :tenantID URL param (for API key auth on tenant-scoped routes).
// Returns empty string when using API key auth on a route with no :tenantID in path.
func tenantIDFromContext(c *gin.Context) string {
	if id, ok := c.Get("tenantID"); ok {
		return id.(string)
	}
	return c.Param("tenantID")
}

// resolveTenantIDFilter returns the effective tenant ID for log queries.
// If JWT set tenantID in context and a tenant_id query param is also provided,
// they must match — otherwise abort with 403.
func resolveTenantIDFilter(c *gin.Context) (string, bool) {
	ctxTenantID := tenantIDFromContext(c)
	queryTenantID := c.Query("tenant_id")
	if ctxTenantID != "" && queryTenantID != "" && ctxTenantID != queryTenantID {
		AbortWithError(c, http.StatusForbidden, ErrorResponse{
			Code:    http.StatusForbidden,
			Message: "tenant_id query parameter does not match authenticated tenant",
		})
		return "", false
	}
	if ctxTenantID != "" {
		return ctxTenantID, true
	}
	return queryTenantID, true
}

// tenantFromContext returns the resolved tenant from context, if present.
// Returns nil when the request is not JWT-authenticated or the route doesn't require a tenant.
func tenantFromContext(c *gin.Context) *models.Tenant {
	if t, ok := c.Get("tenant"); ok {
		return t.(*models.Tenant)
	}
	return nil
}

// mustTenantFromContext returns the resolved tenant from context, panicking if absent.
// Only use on routes where RequireTenant is true.
func mustTenantFromContext(c *gin.Context) *models.Tenant {
	tenant, ok := c.Get("tenant")
	if !ok {
		panic("mustTenantFromContext: tenant not found in context - route is likely missing RequireTenant")
	}
	return tenant.(*models.Tenant)
}
