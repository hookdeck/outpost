package api

import (
	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/emetrics"
)

func MetricsMiddleware() gin.HandlerFunc {
	emeter, err := emetrics.New()
	if err != nil {
		panic(err)
	}
	return func(c *gin.Context) {
		defer func() {
			latency := GetRequestLatency(c)
			emeter.APICalls(c.Request.Context(), emetrics.APICallsOpts{
				Method: c.Request.Method,
				Path:   c.FullPath(),
			})
			emeter.APIResponseLatency(c.Request.Context(), latency, emetrics.APIResponseLatencyOpts{
				Method: c.Request.Method,
				Path:   c.FullPath(),
			})
		}()
		c.Next()
	}
}
