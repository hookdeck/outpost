package apirouter

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPIVersionFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name  string
		value any // stored under apiVersionKey; nil stores nothing
		want  apiVersion
	}{
		{"outside a version group", nil, apiV1},
		{"v1", apiV1, apiV1},
		{"v2", apiV2, apiV2},
		{"a value of another type", 2, apiV1},
		{"the zero version", apiVersion(0), apiV1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tt.value != nil {
				c.Set(apiVersionKey, tt.value)
			}
			if got := apiVersionFromContext(c); got != tt.want {
				t.Errorf("apiVersionFromContext() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAPIVersion_BasePath(t *testing.T) {
	for v, want := range map[apiVersion]string{apiV1: "/api/v1", apiV2: "/api/v2"} {
		if got := v.basePath(); got != want {
			t.Errorf("apiVersion(%d).basePath() = %q, want %q", v, got, want)
		}
	}
}

// Routes are registered from one table into a group per version. Each route
// sees its group's version, from its middlewares on, and a route is only
// served from its MinVersion.
func TestRegisterRoutes_Versions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	writeVersion := func(c *gin.Context) {
		c.String(http.StatusOK, "handler v%d", apiVersionFromContext(c))
	}
	routes := []RouteDefinition{
		{Method: http.MethodGet, Path: "/every", Handler: writeVersion},
		{Method: http.MethodGet, Path: "/from-v1", Handler: writeVersion, MinVersion: apiV1},
		{Method: http.MethodGet, Path: "/from-v2", Handler: writeVersion, MinVersion: apiV2},
		{Method: http.MethodGet, Path: "/middleware", Handler: writeVersion, Middlewares: []gin.HandlerFunc{
			func(c *gin.Context) {
				c.String(http.StatusOK, "middleware v%d", apiVersionFromContext(c))
				c.Abort()
			},
		}},
	}

	r := gin.New()
	for _, v := range apiVersions {
		// No API key configured: every request is admin, no tenant lookup.
		registerRoutes(r.Group(v.basePath(), apiVersionMiddleware(v)), v, RouterConfig{}, nil, routes)
	}

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/api/v1/every", http.StatusOK, "handler v1"},
		{"/api/v2/every", http.StatusOK, "handler v2"},
		{"/api/v1/from-v1", http.StatusOK, "handler v1"},
		{"/api/v2/from-v1", http.StatusOK, "handler v2"},
		{"/api/v1/from-v2", http.StatusNotFound, ""},
		{"/api/v2/from-v2", http.StatusOK, "handler v2"},
		{"/api/v1/middleware", http.StatusOK, "middleware v1"},
		{"/api/v2/middleware", http.StatusOK, "middleware v2"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && w.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", w.Body.String(), tt.wantBody)
			}
		})
	}
}

// Publish is exempt from the body limit in every version, and only publish.
func TestPublishRoutes(t *testing.T) {
	want := []string{"/api/v1/publish", "/api/v2/publish"}
	if !slices.Equal(publishRoutes, want) {
		t.Errorf("publishRoutes = %q, want %q", publishRoutes, want)
	}
}
