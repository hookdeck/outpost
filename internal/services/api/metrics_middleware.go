package api

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/emetrics"
)

func MetricsMiddleware() gin.HandlerFunc {
	emeter, err := emetrics.New()
	if err != nil {
		panic(err)
	}
	return func(c *gin.Context) {
		start := time.Now()
		defer func() {
			emeter.APICalls(c.Request.Context(), emetrics.APICallsOpts{
				Method: c.Request.Method,
				Path:   c.FullPath(),
			})
			emeter.APIResponseLatency(c.Request.Context(), time.Since(start), emetrics.APIResponseLatencyOpts{
				Method: c.Request.Method,
				Path:   c.FullPath(),
			})
		}()
		c.Next()
	}
}
