package tenant

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

func RequireTenantMiddleware(logger *otelzap.Logger, model *TenantModel) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Param("tenantID")
		tenant, err := model.Get(c.Request.Context(), tenantID)
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

func MustTenantFromContext(c *gin.Context) *Tenant {
	tenant, ok := c.Get("tenant")
	if !ok {
		return nil
	}
	return tenant.(*Tenant)
}
