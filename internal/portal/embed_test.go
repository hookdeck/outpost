package portal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAddRoutes_NoRoute_APIReturnsJSON404(t *testing.T) {
	t.Parallel()

	t.Run("embedded mode returns JSON 404 for unmatched API routes", func(t *testing.T) {
		t.Parallel()

		router := gin.New()
		AddRoutes(router, PortalConfig{})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/nonexistent", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, float64(http.StatusNotFound), response["status"])
		assert.Equal(t, "not found", response["message"])
	})

	t.Run("proxy mode returns JSON 404 for unmatched API routes", func(t *testing.T) {
		t.Parallel()

		router := gin.New()
		AddRoutes(router, PortalConfig{
			ProxyURL: "http://localhost:19999",
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/nonexistent", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, float64(http.StatusNotFound), response["status"])
		assert.Equal(t, "not found", response["message"])
	})
}

func TestAddRoutes_NoRoute_APIRootReturnsJSON404(t *testing.T) {
	t.Parallel()

	configs := map[string]PortalConfig{
		"embedded mode": {},
		"proxy mode":    {ProxyURL: "http://localhost:19999"},
	}

	for name, config := range configs {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(name+" "+method, func(t *testing.T) {
				t.Parallel()

				router := gin.New()
				AddRoutes(router, config)

				w := httptest.NewRecorder()
				req, _ := http.NewRequest(method, "/api", nil)
				router.ServeHTTP(w, req)

				assert.Equal(t, http.StatusNotFound, w.Code)
				assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

				var response map[string]interface{}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

				assert.Equal(t, float64(http.StatusNotFound), response["status"])
				assert.Equal(t, "not found", response["message"])
			})
		}
	}
}
