package apirouter

import (
	"errors"
	"net/http"
	"slices"

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

// publishRoutes holds the publish route of every API version.
var publishRoutes = func() []string {
	routes := make([]string, len(apiVersions))
	for i, v := range apiVersions {
		routes[i] = v.basePath() + publishPath
	}
	return routes
}()

// isBodyLimitedRequest reports whether the request is a write other than
// publish. Those are the requests whose body is capped and, for a 5xx, logged.
// Publish is matched on the exact route, so an ID that ends in "publish" is
// limited like any other.
func isBodyLimitedRequest(c *gin.Context) bool {
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return !slices.Contains(publishRoutes, c.FullPath())
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
