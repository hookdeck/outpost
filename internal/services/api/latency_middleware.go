package api

import (
	"time"

	"github.com/gin-gonic/gin"
)

const latencyContextKey = "latencymiddleware_duration"

func LatencyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)
		c.Set(latencyContextKey, duration)
	}
}

// GetRequestLatency returns the request latency from the context
func GetRequestLatency(c *gin.Context) time.Duration {
	if v, exists := c.Get(latencyContextKey); exists {
		if duration, ok := v.(time.Duration); ok {
			return duration
		}
	}
	return 0
}
