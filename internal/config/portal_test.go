package config_test

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/portal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortalShowMCPDestinations(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		env  map[string]string
		want bool
	}{
		{name: "default", want: true},
		{name: "env false", env: map[string]string{"PORTAL_SHOW_MCP_DESTINATIONS": "false"}, want: false},
		{name: "env true", env: map[string]string{"PORTAL_SHOW_MCP_DESTINATIONS": "true"}, want: true},
		// caarlos0/env ignores a present-but-empty variable, so the default stays.
		{name: "env empty", env: map[string]string{"PORTAL_SHOW_MCP_DESTINATIONS": ""}, want: true},
		{name: "yaml false", yaml: "portal:\n  show_mcp_destinations: false\n", want: false},
		{name: "yaml portal without the key keeps the default", yaml: "portal:\n  referer_url: https://example.com\n", want: true},
		{
			name: "env overrides yaml",
			yaml: "portal:\n  show_mcp_destinations: false\n",
			env:  map[string]string{"PORTAL_SHOW_MCP_DESTINATIONS": "true"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOS := &mockOS{
				files:   map[string][]byte{},
				envVars: map[string]string{},
			}
			if tt.yaml != "" {
				mockOS.files["config.yaml"] = []byte(tt.yaml)
				mockOS.envVars["CONFIG"] = "config.yaml"
			}
			for k, v := range tt.env {
				mockOS.envVars[k] = v
			}

			cfg, err := config.ParseWithoutValidation(config.Flags{}, mockOS)
			require.NoError(t, err)

			assert.Equal(t, tt.want, cfg.Portal.ShowMCPDestinations)
			want := "false"
			if tt.want {
				want = "true"
			}
			assert.Equal(t, want, cfg.GetPortalConfig().Configs["SHOW_MCP_DESTINATIONS"])
		})
	}
}

// PORTAL_CONFIGS is served unauthenticated, so its key set is pinned: adding
// a key must be a deliberate change to this test.
func TestGetPortalConfigKeys(t *testing.T) {
	cfg := &config.Config{}
	cfg.InitDefaults()

	keys := make([]string, 0)
	for k := range cfg.GetPortalConfig().Configs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	assert.Equal(t, []string{
		"BRAND_COLOR",
		"DISABLE_OUTPOST_BRANDING",
		"DISABLE_TELEMETRY",
		"ENABLE_DESTINATION_FILTER",
		"ENABLE_WEBHOOK_CUSTOM_HEADERS",
		"FAVICON_URL",
		"FORCE_THEME",
		"LOGO",
		"LOGO_DARK",
		"ORGANIZATION_NAME",
		"PROXY_URL",
		"REFERER_URL",
		"REFRESH_URL",
		"SHOW_MCP_DESTINATIONS",
		"TOPICS",
		"TOPICS_ALLOW_WILDCARDS",
	}, keys)
}

func TestPortalConfigScriptIncludesShowMCPDestinations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, show := range []bool{true, false} {
		cfg := &config.Config{}
		cfg.InitDefaults()
		cfg.Portal.ShowMCPDestinations = show

		router := gin.New()
		portal.AddRoutes(router, cfg.GetPortalConfig(), func(c *gin.Context) {})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/inject-portal-config.js", nil)
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		want := `"SHOW_MCP_DESTINATIONS":"false"`
		if show {
			want = `"SHOW_MCP_DESTINATIONS":"true"`
		}
		assert.Contains(t, w.Body.String(), want)
	}
}
