package portal

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAddRoutes_NoRoute(t *testing.T) {
	t.Parallel()

	const apiNotFoundBody = "from the API not-found handler"
	apiNotFound := func(c *gin.Context) {
		c.String(http.StatusNotFound, apiNotFoundBody)
	}

	configs := map[string]PortalConfig{
		"embedded mode": {},
		"proxy mode":    {ProxyURL: "http://localhost:19999"},
	}

	for name, config := range configs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			do := func(method, path string) *httptest.ResponseRecorder {
				router := gin.New()
				AddRoutes(router, config, apiNotFound)

				w := httptest.NewRecorder()
				req, _ := http.NewRequest(method, path, nil)
				router.ServeHTTP(w, req)
				return w
			}

			apiRequests := []struct{ method, path string }{
				{http.MethodGet, "/api/v1/nonexistent"},
				{http.MethodPost, "/api/v1/nonexistent"},
				{http.MethodGet, "/api"},
				{http.MethodPost, "/api"},
			}
			for _, r := range apiRequests {
				t.Run(r.method+" "+r.path+" goes to the API not-found handler", func(t *testing.T) {
					w := do(r.method, r.path)

					assert.Equal(t, http.StatusNotFound, w.Code)
					assert.Equal(t, apiNotFoundBody, w.Body.String())
				})
			}

			for _, path := range []string{"/apidocs", "/destinations/des_1"} {
				t.Run("POST "+path+" does not go to the API not-found handler", func(t *testing.T) {
					w := do(http.MethodPost, path)

					assert.NotEqual(t, apiNotFoundBody, w.Body.String())
				})
			}
		})
	}
}
