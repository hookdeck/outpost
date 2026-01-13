package apirouter

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// DeprecationWarningMiddleware adds deprecation headers for deprecated API paths
// that have been superseded by /tenants/* paths.
// Clients should migrate to the new paths indicated in the Link header.
func DeprecationWarningMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		oldPath := c.Request.URL.Path
		newPath := strings.Replace(oldPath, "/api/v1/", "/api/v1/tenants/", 1)

		// Add deprecation headers (RFC 8594)
		c.Header("Deprecation", "true")
		c.Header("Link", fmt.Sprintf("<%s>; rel=\"successor-version\"", newPath))

		c.Next()
	}
}
