package services

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/worker"
)

// HealthHandler creates a health check handler that reports worker supervisor health
func HealthHandler(supervisor *worker.WorkerSupervisor) gin.HandlerFunc {
	return func(c *gin.Context) {
		tracker := supervisor.GetHealthTracker()
		status := tracker.GetStatus()
		if tracker.IsHealthy() {
			c.JSON(http.StatusOK, status)
		} else {
			c.JSON(http.StatusServiceUnavailable, status)
		}
	}
}

// NewBaseRouter creates a base router with health check endpoint
// This is used by all services to expose /healthz
//
// TODO: Rethink API versioning strategy in the future.
// For now, we expose health check at both /healthz and /api/v1/healthz for backwards compatibility.
// The /api/v1 prefix is hardcoded here but should be part of a broader versioning approach.
func NewBaseRouter(supervisor *worker.WorkerSupervisor, ginMode string) *gin.Engine {
	gin.SetMode(ginMode)
	r := gin.New()
	r.Use(gin.Recovery())

	healthHandler := HealthHandler(supervisor)
	r.GET("/healthz", healthHandler)
	r.GET("/api/v1/healthz", healthHandler)

	return r
}
