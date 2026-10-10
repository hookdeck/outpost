package apirouter

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxRequestBodyBytes is the largest request body accepted outside the publish endpoint
const MaxRequestBodyBytes = 1 << 20

// RequestBodyLimitMiddleware rejects request bodies over MaxRequestBodyBytes
// on write requests other than publish. A declared length over the limit is
// answered without reading the body. Otherwise reading stops at the limit and
// the handler's bind error is turned into the same response.
func RequestBodyLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isBodyLimitedRequest(c) || c.Request.Body == nil {
			c.Next()
			return
		}
		if declaresOversizedBody(c) {
			AbortWithError(c, http.StatusRequestEntityTooLarge, NewErrRequestBodyTooLarge())
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxRequestBodyBytes)
		c.Next()
	}
}

// isBodyLimitedRequest reports whether the request is a write other than
// publish. Those are the requests whose body is capped and, for a 5xx, logged.
func isBodyLimitedRequest(c *gin.Context) bool {
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return c.FullPath() != apiBasePath+publishPath
	}
	return false
}

func declaresOversizedBody(c *gin.Context) bool {
	return c.Request.ContentLength > MaxRequestBodyBytes
}

func isRequestBodyTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}
