package apirouter

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// TextParamsMiddleware rejects a request whose path or query parameters
// contain a NUL byte or invalid UTF-8. No ID or filter value can hold one,
// and PostgreSQL refuses it in a text parameter (SQLSTATE 22021), which would
// otherwise surface as a 500 from the PostgreSQL log store.
func TextParamsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, param := range c.Params {
			if !isValidText(param.Value) {
				abortWithInvalidText(c, param.Key)
				return
			}
		}
		query := c.Request.URL.Query()
		for _, key := range slices.Sorted(maps.Keys(query)) {
			for _, value := range query[key] {
				if !isValidText(value) {
					abortWithInvalidText(c, key)
					return
				}
			}
		}
		c.Next()
	}
}

func isValidText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func abortWithInvalidText(c *gin.Context, name string) {
	if !isValidText(name) {
		name = "parameter"
	}
	err := fmt.Errorf("invalid %s: must be valid UTF-8 without NUL bytes", name)
	AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
}
