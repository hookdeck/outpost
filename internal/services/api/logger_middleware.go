package api

import (
	"github.com/gin-gonic/gin"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

func LoggerMiddleware(logger *otelzap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := logger.Ctx(c.Request.Context())
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := GetRequestLatency(c)

		if len(c.Errors) > 0 {
			// Log errors
			logger.Error("request failed",
				zap.String("path", path),
				zap.String("query", query),
				zap.String("method", c.Request.Method),
				zap.Int("status", c.Writer.Status()),
				zap.Duration("latency", latency),
				zap.String("client_ip", c.ClientIP()),
				zap.Strings("errors", c.Errors.Errors()),
			)
		} else {
			// Log successful requests
			logger.Info("request completed",
				zap.String("path", path),
				zap.String("query", query),
				zap.String("method", c.Request.Method),
				zap.Int("status", c.Writer.Status()),
				zap.Duration("latency", latency),
				zap.String("client_ip", c.ClientIP()),
			)
		}
	}
}
