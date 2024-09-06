package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

func RequireTenantMiddleware(logger *otelzap.Logger, cmdable redis.Cmdable, model *models.TenantModel) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Param("tenantID")
		tenant, err := model.Get(c.Request.Context(), cmdable, tenantID)
		if err != nil {
			logger.Error("failed to get tenant", zap.Error(err))
			c.AbortWithStatus(http.StatusInternalServerError)
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
