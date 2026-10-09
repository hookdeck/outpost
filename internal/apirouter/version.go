package apirouter

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// apiVersion is a major version of the API, served under /api/v<N>. Every
// version is registered from the same route table; a handler that behaves
// differently per version reads apiVersionFromContext.
type apiVersion int

const (
	apiV1 apiVersion = 1
	apiV2 apiVersion = 2
)

// apiVersions lists the served versions, oldest first.
var apiVersions = []apiVersion{apiV1, apiV2}

// apiVersionKey is the gin context key holding the request's apiVersion.
const apiVersionKey = "apiVersion"

func (v apiVersion) basePath() string {
	return "/api/v" + strconv.Itoa(int(v))
}

// apiVersionMiddleware records the version of its route group. It must be
// added to the group before the routes are: gin copies a group's handlers into
// each route when the route is registered.
func apiVersionMiddleware(v apiVersion) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(apiVersionKey, v)
	}
}

// apiVersionFromContext returns the version of the route group serving the
// request, or v1 outside a version group.
func apiVersionFromContext(c *gin.Context) apiVersion {
	if v, ok := c.Get(apiVersionKey); ok {
		if version, ok := v.(apiVersion); ok && version > 0 {
			return version
		}
	}
	return apiV1
}
