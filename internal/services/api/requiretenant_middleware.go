package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

func RequireTenantMiddleware(logger *otelzap.Logger, model *models.TenantModel) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.Param("tenantID")
		fmt.Println("=================================================================", tenantID)
		tenant, err := model.Get(c.Request.Context(), tenantID)
		fmt.Println("=================================================================", tenantID, tenant)
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
