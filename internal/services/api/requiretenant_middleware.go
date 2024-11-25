package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

func RequireTenantMiddleware(logger *otelzap.Logger, entityStore models.EntityStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Param("tenantID")
		tenant, err := entityStore.RetrieveTenant(c.Request.Context(), tenantID)
		if err != nil {
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
		return nil
	}
	return tenant.(*models.Tenant)
}
