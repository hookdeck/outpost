package api

import (
	"fmt"
	"math"
	"strings"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func LoggerMiddleware(logger *logging.Logger) gin.HandlerFunc {
	return LoggerMiddlewareWithSanitizer(logger, nil)
}

func LoggerMiddlewareWithSanitizer(logger *logging.Logger, sanitizer *RequestBodySanitizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip logging portal requests
		if !strings.Contains(c.Request.URL.Path, "api/v") {
			c.Next()
			return
		}

		logger := logger.Ctx(c.Request.Context()).WithOptions(zap.AddStacktrace(zap.FatalLevel))

		// Buffer request body if we have a sanitizer and this might be a destination request
		var bufferedBody *BufferedReader
		var requestBodyFields []zap.Field

		if sanitizer != nil && shouldBufferRequestBody(c) {
			if br, err := NewBufferedReader(c.Request.Body); err == nil {
				bufferedBody = br
				// Replace the request body with a new reader so the handler can still read it
				c.Request.Body = br.NewReadCloser()
			}
		}

		c.Next()

		fields := []zap.Field{}
		fields = append(fields, basicFields(c)...)
		fields = append(fields, pathFields(c)...)
		fields = append(fields, queryFields(c)...)
		fields = append(fields, errorFields(c)...)

		// Add sanitized request body for 5xx errors
		if c.Writer.Status() >= 500 && bufferedBody != nil {
			requestBodyFields = getRequestBodyFields(bufferedBody, sanitizer)
			fields = append(fields, requestBodyFields...)
		}

		if c.Writer.Status() >= 500 {
			logger.Error("request completed", fields...)

			if hub := sentrygin.GetHubFromContext(c); hub != nil {
				hub.CaptureException(getErrorWithStackTrace(c.Errors.Last().Err))
			}
		} else {
			if strings.HasPrefix(c.Request.URL.Path, "/api") && strings.HasSuffix(c.Request.URL.Path, "/healthz") {
				logger.Debug("healthz request completed", fields...)
			} else {
				logger.Info("request completed", fields...)
			}
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

func getErrorWithStackTrace(err error) error {
	if errResp, ok := err.(ErrorResponse); ok {
		return errResp.Err
	}
	return err
}

// shouldBufferRequestBody determines if we should buffer the request body for potential logging
func shouldBufferRequestBody(c *gin.Context) bool {
	// Only buffer POST, PUT, PATCH requests that might contain request bodies
	method := c.Request.Method
	if method != "POST" && method != "PUT" && method != "PATCH" {
		return false
	}

	// Exclude publish endpoints since they contain user data that shouldn't be logged. We could consider making this configurable in the future.
	path := c.Request.URL.Path
	if strings.Contains(path, "/publish") {
		return false
	}

	// Buffer all other POST/PUT/PATCH requests for potential 5xx error logging
	return true
}

// getRequestBodyFields creates log fields for sanitized request body
func getRequestBodyFields(bufferedBody *BufferedReader, sanitizer *RequestBodySanitizer) []zap.Field {
	if bufferedBody == nil || sanitizer == nil {
		return []zap.Field{
			zap.String("request_body", "[NO_BODY]"),
		}
	}

	sanitizedBody, err := sanitizer.SanitizeRequestBody(bufferedBody.NewReader())
	if err != nil {
		return []zap.Field{
			zap.String("request_body_error", err.Error()),
		}
	}

	if len(sanitizedBody) == 0 {
		return []zap.Field{
			zap.String("request_body", "[EMPTY_BODY]"),
		}
	}

	return []zap.Field{
		zap.String("request_body", string(sanitizedBody)),
	}
}
