package api

import (
	"math"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

func LoggerMiddleware(logger *otelzap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create a logger without stacktrace for errors
		logger := logger.Ctx(c.Request.Context())

		c.Next()

		// Get request latency in ms, rounded to 2 decimal places
		latencyMs := float64(GetRequestLatency(c)) / float64(time.Millisecond)
		latencyMs = math.Round(latencyMs*100) / 100

		// Keep both normalized and raw paths
		rawPath := c.Request.URL.Path
		normalizedPath := rawPath
		params := make(map[string]string)
		for _, p := range c.Params {
			normalizedPath = strings.Replace(normalizedPath, p.Value, ":"+p.Key, 1)
			params[p.Key] = p.Value
		}

		fields := []zap.Field{
			zap.String("path", normalizedPath),
			zap.String("raw_path", rawPath),
			zap.String("query", c.Request.URL.RawQuery),
			zap.String("method", c.Request.Method),
			zap.Int("status", c.Writer.Status()),
			zap.Float64("latency_ms", latencyMs),
			zap.String("client_ip", c.ClientIP()),
		}

		// Only add params if we have any
		if len(params) > 0 {
			fields = append(fields, zap.Any("params", params))
		}

		status := c.Writer.Status()
		hasErrors := len(c.Errors) > 0 || status >= 400

		if hasErrors {
			// Extract error messages if any
			if len(c.Errors) > 0 {
				var errs []string
				for _, err := range c.Errors {
					errs = append(errs, err.Err.Error())
				}
				fields = append(fields, zap.Strings("errors", errs))
			}

			// Keep error log clean with just essential info
			if status >= 500 {
				// For 5xx errors, log as error with stacktrace
				logger.Error("request completed", fields...)
			} else {
				// Everything else (including 4xx) logs as info
				logger.Info("request completed", fields...)
			}
		} else {
			logger.Info("request completed", fields...)
		}
	}
}
