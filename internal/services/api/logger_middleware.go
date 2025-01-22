package api

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func LoggerMiddleware(logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := logger.Ctx(c.Request.Context()).WithOptions(zap.AddStacktrace(zap.FatalLevel))
		c.Next()

		fields := []zap.Field{}
		fields = append(fields, basicFields(c)...)
		fields = append(fields, pathFields(c)...)
		fields = append(fields, queryFields(c)...)
		fields = append(fields, errorFields(c)...)

		if c.Writer.Status() >= 500 {
			logger.Error("request completed", fields...)
		} else {
			logger.Info("request completed", fields...)
		}
	}
}

func basicFields(c *gin.Context) []zap.Field {
	return []zap.Field{
		zap.String("method", c.Request.Method),
		zap.Int("status", c.Writer.Status()),
		zap.Float64("latency_ms", math.Round(float64(GetRequestLatency(c))/float64(time.Millisecond)*100)/100),
		zap.String("client_ip", c.ClientIP()),
	}
}

func pathFields(c *gin.Context) []zap.Field {
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
	}

	if len(params) > 0 {
		fields = append(fields, zap.Any("params", params))
	}

	return fields
}

func queryFields(c *gin.Context) []zap.Field {
	if c.Request.URL.RawQuery == "" {
		return nil
	}
	return []zap.Field{
		zap.String("query", c.Request.URL.RawQuery),
	}
}

func errorFields(c *gin.Context) []zap.Field {
	if len(c.Errors) == 0 {
		return nil
	}

	err := c.Errors.Last().Err
	if c.Writer.Status() >= 500 {
		return getErrorFields(err)
	}
	return []zap.Field{
		zap.String("error", err.Error()),
	}
}

func getErrorFields(err error) []zap.Field {
	var originalErr error

	// If it's our ErrorResponse, get the inner error
	if errResp, ok := err.(ErrorResponse); ok {
		err = errResp.Err
	}

	// Keep the wrapped error for stack trace but get original for type/message
	wrappedErr := err
	if cause := errors.Unwrap(err); cause != nil {
		originalErr = cause
	} else {
		originalErr = err
	}

	fields := []zap.Field{
		zap.String("error", originalErr.Error()),
		zap.String("error_type", fmt.Sprintf("%T", originalErr)),
	}

	// Get the stack trace from the wrapped error
	type stackTracer interface {
		StackTrace() errors.StackTrace
	}
	var st stackTracer
	if errors.As(wrappedErr, &st) {
		trace := fmt.Sprintf("%+v", st.StackTrace())
		lines := strings.Split(trace, "\n")
		var filtered []string

		for i := 0; i < len(lines); i++ {
			line := lines[i]
			if strings.Contains(line, "github.com/hookdeck/outpost") {
				filtered = append(filtered, line)
				// Include the next line if it's a file path
				if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "\t") {
					filtered = append(filtered, lines[i+1])
				}
				// Stop at the first handler
				if strings.Contains(line, "Handler") {
					break
				}
			}
		}

		if len(filtered) > 0 {
			fields = append(fields, zap.String("error_trace", strings.Join(filtered, "\n")))
		}
	}

	return fields
}
