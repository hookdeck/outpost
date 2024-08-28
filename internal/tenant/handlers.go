package tenant

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type TenantHandlers struct {
	logger    *otelzap.Logger
	model     *TenantModel
	jwtSecret string
}

func NewHandlers(logger *otelzap.Logger, redisClient *redis.Client, jwtSecret string) *TenantHandlers {
	return &TenantHandlers{
		logger:    logger,
		model:     NewTenantModel(redisClient),
		jwtSecret: jwtSecret,
	}
}

func (h *TenantHandlers) Upsert(c *gin.Context) {
	logger := h.logger.Ctx(c.Request.Context())
	tenantID := c.Param("tenantID")

	// Check existing tenant.
	tenant, err := h.model.Get(c.Request.Context(), tenantID)
	if err != nil {
		logger.Error("failed to get tenant", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}

	// If tenant already exists, return.
	if tenant != nil {
		c.JSON(http.StatusOK, tenant)
		return
	}

	// Create new tenant.
	tenant = &Tenant{
		ID:        tenantID,
		CreatedAt: time.Now(),
	}
	if err := h.model.Set(c.Request.Context(), *tenant); err != nil {
		logger.Error("failed to set tenant", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusCreated, tenant)
}

func (h *TenantHandlers) Retrieve(c *gin.Context) {
	logger := h.logger.Ctx(c.Request.Context())
	tenantID := c.Param("tenantID")
	tenant, err := h.model.Get(c.Request.Context(), tenantID)
	if err != nil {
		logger.Error("failed to get tenant", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	if tenant == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, tenant)
}

func (h *TenantHandlers) Delete(c *gin.Context) {
	logger := h.logger.Ctx(c.Request.Context())
	tenantID := c.Param("tenantID")
	tenant, err := h.model.Get(c.Request.Context(), tenantID)
	if err != nil {
		logger.Error("failed to get tenant", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	if tenant == nil {
		c.Status(http.StatusNotFound)
		return
	}
	tenant, err = h.model.Clear(c.Request.Context(), tenantID)
	if err != nil {
		logger.Error("failed to delete tenant", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}

	// TODO: delete associated destinations

	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *TenantHandlers) RetrievePortal(c *gin.Context) {
	logger := h.logger.Ctx(c.Request.Context())
	tenantID := c.Param("tenantID")
	tenant, err := h.model.Get(c.Request.Context(), tenantID)
	if err != nil {
		logger.Error("failed to get tenant", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	if tenant == nil {
		c.Status(http.StatusNotFound)
		return
	}
	jwtToken, err := JWT.New(h.jwtSecret, tenantID)
	if err != nil {
		logger.Error("failed to create jwt token", zap.Error(err))
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"redirect_url": "https://example.com?token=" + jwtToken,
	})
}
