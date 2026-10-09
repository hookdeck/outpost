package services

import (
	"net/http"
	"net/http/pprof"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/apirouter"
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
// The health check is also served under each API version prefix, so a client
// configured with a versioned base URL can reach it. /api/v2/healthz also lets
// a client detect whether the server serves API v2.
func NewBaseRouter(supervisor *worker.WorkerSupervisor, ginMode string, pprofEnabled bool) *gin.Engine {
	gin.SetMode(ginMode)
	r := gin.New()
	r.Use(apirouter.RecoveryMiddleware())

	healthHandler := HealthHandler(supervisor)
	r.GET("/healthz", healthHandler)
	r.GET("/api/v1/healthz", healthHandler)
	r.GET("/api/v2/healthz", healthHandler)

	if pprofEnabled {
		registerPprof(r)
	}

	return r
}

// registerPprof mounts net/http/pprof under /debug/pprof/. The handlers are
// unauthenticated, so this is opt-in via config.
func registerPprof(r *gin.Engine) {
	r.GET("/debug/pprof/", gin.WrapF(pprof.Index))
	r.GET("/debug/pprof/cmdline", gin.WrapF(pprof.Cmdline))
	r.GET("/debug/pprof/profile", gin.WrapF(pprof.Profile))
	r.GET("/debug/pprof/symbol", gin.WrapF(pprof.Symbol))
	r.GET("/debug/pprof/trace", gin.WrapF(pprof.Trace))
	r.GET("/debug/pprof/:name", func(c *gin.Context) {
		pprof.Handler(c.Param("name")).ServeHTTP(c.Writer, c.Request)
	})
}
