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

// AuthOptions configures the behaviour of AuthMiddleware.
type AuthOptions struct {
	AdminOnly     bool
	RequireTenant bool
}

// AuthMiddleware returns a single gin.HandlerFunc that handles authentication,
// authorization, and tenant resolution for every route.
//
// Flow:
//  1. VPC mode (apiKey=""): grant admin, resolve tenant if RequireTenant, done.
//  2. Validate auth header → 401 if missing/malformed.
//  3. token == apiKey → admin, resolve tenant if RequireTenant, done.
//  4. JWT.Extract(token) → 401 if invalid.
//  5. AdminOnly? → 403.
//  6. :tenantID param mismatch? → 403.
//  7. Set tenantID + RoleTenant, always resolve tenant for JWT → 401 if missing/deleted.
func AuthMiddleware(apiKey, jwtSecret string, tenantRetriever TenantRetriever, opts AuthOptions) gin.HandlerFunc {
	// VPC mode — no API key configured, everything is admin.
	if apiKey == "" {
		return func(c *gin.Context) {
			c.Set(authRoleKey, RoleAdmin)
			if opts.RequireTenant {
				resolveTenantOrAbort(c, tenantRetriever, tenantIDFromContext(c), false)
				if c.IsAborted() {
					return
				}
			}
			c.Next()
		}
	}

	return func(c *gin.Context) {
		// 2. Validate auth header
		token, err := validateAuthHeader(c)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 3. API key match → admin
		if token == apiKey {
			c.Set(authRoleKey, RoleAdmin)
			if opts.RequireTenant {
				resolveTenantOrAbort(c, tenantRetriever, tenantIDFromContext(c), false)
				if c.IsAborted() {
					return
				}
			}
			c.Next()
			return
		}

		// 4. Try JWT
		claims, err := JWT.Extract(jwtSecret, token)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 5. AdminOnly routes reject JWT tokens
		if opts.AdminOnly {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		// 6. tenantID param mismatch
		if paramTenantID := c.Param("tenantID"); paramTenantID != "" && paramTenantID != claims.TenantID {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		// 7. Set tenant context and always resolve for JWT
		c.Set("tenantID", claims.TenantID)
		c.Set(authRoleKey, RoleTenant)
		resolveTenantOrAbort(c, tenantRetriever, claims.TenantID, true)
		if c.IsAborted() {
			return
		}

		c.Next()
	}
}

// resolveTenantOrAbort looks up the tenant and sets it in context.
// When isJWT is true, a missing or deleted tenant results in 401 (token is stale).
// When isJWT is false, a missing or deleted tenant results in 404.
func resolveTenantOrAbort(c *gin.Context, retriever TenantRetriever, tenantID string, isJWT bool) {
	if tenantID == "" {
		if isJWT {
			c.AbortWithStatus(http.StatusUnauthorized)
		} else {
			AbortWithError(c, http.StatusNotFound, NewErrNotFound("tenant"))
		}
		return
	}

	tenant, err := retriever.RetrieveTenant(c.Request.Context(), tenantID)
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
