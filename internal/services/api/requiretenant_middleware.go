package api

import (
	"net/http"

	"errors"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/models"
)

func RequireTenantMiddleware(entityStore models.EntityStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, exists := c.Get("tenantID")
		if !exists {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		tenant, err := entityStore.RetrieveTenant(c.Request.Context(), tenantID.(string))
		if err != nil {
			if err == models.ErrTenantDeleted {
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
		AbortWithError(c, http.StatusInternalServerError, errors.New("tenant not found in context"))
		return nil
	}
	return tenant.(*models.Tenant)
}
