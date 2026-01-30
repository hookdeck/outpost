package apirouter

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
)

// TenantRetriever is satisfied by tenantstore.TenantStore.
// Defined here to avoid coupling the router/middleware to the full store interface.
type TenantRetriever interface {
	RetrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error)
}

func RequireTenantMiddleware(tenantRetriever TenantRetriever) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Param("tenantID")
		if tenantID == "" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		tenant, err := tenantRetriever.RetrieveTenant(c.Request.Context(), tenantID)
		if err != nil {
			if err == tenantstore.ErrTenantDeleted {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
			return
		}
		if tenant == nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Set("tenant", tenant)
		c.Next()
	}
}

func mustTenantFromContext(c *gin.Context) *models.Tenant {
	tenant, ok := c.Get("tenant")
	if !ok {
		panic("mustTenantFromContext: tenant not found in context - route is likely missing TenantScoped: true")
	}
	return tenant.(*models.Tenant)
}
