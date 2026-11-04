package apirouter

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxRequestBodyBytes is the largest request body accepted outside the publish endpoint
const MaxRequestBodyBytes = 1 << 20

// RequestBodyLimitMiddleware rejects request bodies over the limit on write
// requests: maxEventSizeBytes for publish (0 means no cap),
// MaxRequestBodyBytes for the rest. A declared length over the limit is
// answered without reading the body. Otherwise reading stops at the limit and
// the handler's bind error is turned into the same response.
func RequestBodyLimitMiddleware(maxEventSizeBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := requestBodyLimit(c, maxEventSizeBytes)
		if limit <= 0 || c.Request.Body == nil {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit {
			AbortWithError(c, http.StatusRequestEntityTooLarge, NewErrRequestBodyTooLarge())
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

func isWriteRequest(c *gin.Context) bool {
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	}
	return false
}

func isPublishRequest(c *gin.Context) bool {
	return c.FullPath() == apiBasePath+publishPath
}

func requestBodyLimit(c *gin.Context, maxEventSizeBytes int64) int64 {
	if !isWriteRequest(c) {
		return 0
	}
	if isPublishRequest(c) {
		return maxEventSizeBytes
	}
	return MaxRequestBodyBytes
}

// logsRequestBody reports whether the start of the body is buffered for the 5xx
// log: writes other than publish, unless the declared length is over the limit.
func logsRequestBody(c *gin.Context) bool {
	return isWriteRequest(c) && !isPublishRequest(c) && c.Request.ContentLength <= MaxRequestBodyBytes
}

func isRequestBodyTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}
