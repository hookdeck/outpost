package config_test

import (
	"bytes"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// requiredEnv is the minimum for Validate to pass.
var requiredEnv = map[string]string{
	"POSTGRES_URL":          "postgres://localhost:5432/outpost",
	"RABBITMQ_SERVER_URL":   "amqp://localhost:5672",
	"AES_ENCRYPTION_SECRET": "secret",
}

func defaultMCPConfig() config.MCPConfig {
	return config.MCPConfig{
		RetrySchedule:          []int{30, 120, 600},
		VerificationTTLSeconds: 86400,
	}
}

func parseMCP(t *testing.T, env map[string]string, yamlDoc string) *config.Config {
	t.Helper()
	all := map[string]string{}
	for k, v := range requiredEnv {
		all[k] = v
	}
	for k, v := range env {
		all[k] = v
	}
	m := &mockOS{files: map[string][]byte{}, envVars: all}
	if yamlDoc != "" {
		m.files["config.yaml"] = []byte(yamlDoc)
		m.envVars["CONFIG"] = "config.yaml"
	}
	cfg, err := config.ParseWithOS(config.Flags{}, m)
	require.NoError(t, err)
	return cfg
}

func TestMCPConfig_Defaults(t *testing.T) {
	cfg := parseMCP(t, nil, "")
	assert.Equal(t, defaultMCPConfig(), cfg.MCP)
	assert.False(t, cfg.MCP.Enabled)
	assert.Empty(t, cfg.MCPWarnings())
}

func TestMCPConfig_EnvAndYAML(t *testing.T) {
	cfg := parseMCP(t, map[string]string{
		"MCP_ENABLED":                  "true",
		"MCP_ALLOW_INSECURE_CALLBACKS": "true",
		"MCP_RETRY_SCHEDULE":           "5,10",
		"MCP_VERIFICATION_TTL_SECONDS": "5400",
	}, "")
	assert.Equal(t, config.MCPConfig{
		Enabled:                true,
		AllowInsecureCallbacks: true,
		RetrySchedule:          []int{5, 10},
		VerificationTTLSeconds: 5400,
	}, cfg.MCP)

	cfg = parseMCP(t, nil, "mcp:\n  enabled: true\n  retry_schedule: [7]\n  verification_ttl_seconds: 3600\n")
	assert.Equal(t, config.MCPConfig{
		Enabled:                true,
		RetrySchedule:          []int{7},
		VerificationTTLSeconds: 3600,
	}, cfg.MCP)
}

func TestMCPConfig_Validation(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"zero schedule entry", map[string]string{"MCP_RETRY_SCHEDULE": "30,0"}, "MCP_RETRY_SCHEDULE"},
		{"negative schedule entry", map[string]string{"MCP_RETRY_SCHEDULE": "-1"}, "MCP_RETRY_SCHEDULE"},
		{"zero ttl", map[string]string{"MCP_VERIFICATION_TTL_SECONDS": "0"}, "MCP_VERIFICATION_TTL_SECONDS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			all := map[string]string{}
			for k, v := range requiredEnv {
				all[k] = v
			}
			for k, v := range tt.env {
				all[k] = v
			}
			all["MCP_ENABLED"] = "true"
			_, err := config.ParseWithOS(config.Flags{}, &mockOS{files: map[string][]byte{}, envVars: all})
			require.Error(t, err)
			assert.ErrorIs(t, err, config.ErrInvalidMCPConfig)
			assert.Contains(t, err.Error(), tt.want)

			delete(all, "MCP_ENABLED")
			_, err = config.ParseWithOS(config.Flags{}, &mockOS{files: map[string][]byte{}, envVars: all})
			assert.NoError(t, err, "keys are not validated while MCP is off")
		})
	}
}

func TestMCPConfig_RetryBackoff(t *testing.T) {
	cfg := parseMCP(t, map[string]string{"MCP_RETRY_SCHEDULE": "5,15,60"}, "")
	b, limit := cfg.MCPRetryBackoff()
	assert.Equal(t, 3, limit)
	assert.Equal(t, time.Duration(cfg.LogBatchThresholdSeconds)*time.Second, b.Duration(0), "raised to one log flush")
	assert.Equal(t, 15*time.Second, b.Duration(1))
	assert.Equal(t, 60*time.Second, b.Duration(2))

	_, global := cfg.GetRetryBackoff()
	assert.Equal(t, cfg.RetryMaxLimit, global, "the global policy is unchanged")
}

func TestMCPConfig_Warnings(t *testing.T) {
	cfg := parseMCP(t, map[string]string{"MCP_ENABLED": "true", "DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST": "localhost", "MCP_ALLOW_INSECURE_CALLBACKS": "true"}, "")
	warnings := strings.Join(cfg.MCPWarnings(), "\n")
	assert.Contains(t, warnings, "MCP_ALLOW_INSECURE_CALLBACKS")
	assert.NotContains(t, warnings, "has no effect")

	cfg = parseMCP(t, map[string]string{"MCP_ENABLED": "true", "MCP_ALLOW_INSECURE_CALLBACKS": "true"}, "")
	assert.Contains(t, strings.Join(cfg.MCPWarnings(), "\n"), "has no effect")

	cfg = parseMCP(t, map[string]string{"MCP_ENABLED": "true", "MCP_RETRY_SCHEDULE": "1,5,60"}, "")
	assert.Equal(t, []string{"MCP_RETRY_SCHEDULE: delays below LOG_BATCH_THRESHOLD_SECONDS (10s) are raised to it: 1s, 5s"}, cfg.MCPWarnings())

	cfg = parseMCP(t, map[string]string{"MCP_ENABLED": "true", "MCP_RETRY_SCHEDULE": "1,5", "LOG_BATCH_THRESHOLD_SECONDS": "0"}, "")
	assert.Empty(t, cfg.MCPWarnings())

	cfg = parseMCP(t, map[string]string{"MCP_ALLOW_INSECURE_CALLBACKS": "true"}, "")
	assert.Empty(t, cfg.MCPWarnings(), "nothing to warn about while MCP is off")
}

// The MCP settings are documented by hand in the configuration reference and
// the example files. These tests keep the names and defaults in step.

// envByYAMLPath maps every YAML key path ("mcp.retry_schedule") of typ to
// its env var name.
func envByYAMLPath(typ reflect.Type, yamlPrefix, envPrefix string, out map[string]string) {
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		yamlName, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if !f.IsExported() || yamlName == "" || yamlName == "-" {
			continue
		}
		path := yamlPrefix + yamlName
		if env := f.Tag.Get("env"); env != "" {
			out[path] = envPrefix + env
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			envByYAMLPath(ft, path+".", envPrefix+f.Tag.Get("envPrefix"), out)
		}
	}
}

func mcpEnvNames() map[string]string {
	byYAML := map[string]string{}
	envByYAMLPath(reflect.TypeOf(config.Config{}), "", "", byYAML)
	out := map[string]string{}
	for path, env := range byYAML {
		if strings.HasPrefix(env, "MCP_") {
			out[env] = path
		}
	}
	return out
}

func TestMCPConfig_MatchesConfigurationReference(t *testing.T) {
	data, err := os.ReadFile("../../docs/content/self-hosting/configuration.mdoc")
	require.NoError(t, err)
	_, section, ok := strings.Cut(string(data), "\n## MCP Events (Experimental)\n")
	require.True(t, ok, "configuration.mdoc has an MCP Events section")
	section, _, _ = strings.Cut(section, "\n## ")

	row := regexp.MustCompile("(?m)^\\| `([A-Z0-9_]+)` \\| (?:`([^`]*)`|—) \\|")
	documented := map[string]bool{}
	defaults := parseMCP(t, nil, "")
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		env, def := m[1], m[2]
		documented[env] = true
		assert.Contains(t, mcpEnvNames(), env, "%s is a config env var", env)
		if def == "" {
			continue
		}
		// Setting the documented default changes nothing.
		cfg := parseMCP(t, map[string]string{env: def}, "")
		assert.Equal(t, defaults.MCP, cfg.MCP, "documented default of %s", env)
	}
	for env := range mcpEnvNames() {
		assert.True(t, documented[env], "%s is documented in the MCP Events table", env)
	}
}

func TestMCPConfig_MatchesEnvExample(t *testing.T) {
	data, err := os.ReadFile("../../.env.example")
	require.NoError(t, err)

	line := regexp.MustCompile(`(?m)^# (MCP_[A-Z0-9_]+)="([^"]*)"$`)
	env := map[string]string{}
	for _, m := range line.FindAllStringSubmatch(string(data), -1) {
		assert.Contains(t, mcpEnvNames(), m[1], "%s in .env.example is a config env var", m[1])
		env[m[1]] = m[2]
	}
	for name := range mcpEnvNames() {
		assert.Contains(t, env, name, "%s is in .env.example", name)
	}

	cfg := parseMCP(t, env, "")
	want := defaultMCPConfig()
	want.Enabled = true
	assert.Equal(t, want, cfg.MCP)
}

func TestMCPConfig_MatchesYAMLExample(t *testing.T) {
	data, err := os.ReadFile("../../.outpost.yaml.example")
	require.NoError(t, err)
	_, section, ok := strings.Cut(string(data), "\n## MCP Events\n")
	require.True(t, ok, ".outpost.yaml.example has an MCP Events section")
	section, _, _ = strings.Cut(section, "\n\n")

	// Uncomment the commented-out keys, leaving prose comments out.
	key := regexp.MustCompile(`^# (\s*[a-z0-9_]+:.*)$`)
	var doc strings.Builder
	for _, l := range strings.Split(section, "\n") {
		if m := key.FindStringSubmatch(l); m != nil {
			doc.WriteString(m[1] + "\n")
		}
	}
	require.Contains(t, doc.String(), "mcp:\n")

	// Every key is a config key.
	dec := yaml.NewDecoder(bytes.NewReader([]byte(doc.String())))
	dec.KnownFields(true)
	var strict config.Config
	require.NoError(t, dec.Decode(&strict))

	cfg := parseMCP(t, nil, doc.String())
	want := defaultMCPConfig()
	want.Enabled = true
	assert.Equal(t, want, cfg.MCP)
}

func TestMCPConfig_RetryPollBackoff(t *testing.T) {
	assert.Equal(t, 30*time.Second, parseMCP(t, nil, "").GetRetryPollBackoff())
	assert.Equal(t, 30*time.Second, parseMCP(t, map[string]string{"MCP_RETRY_SCHEDULE": "2"}, "").GetRetryPollBackoff(), "ignored while MCP is off")
	assert.Equal(t, 10*time.Second, parseMCP(t, map[string]string{"MCP_ENABLED": "true", "MCP_RETRY_SCHEDULE": "2,60"}, "").GetRetryPollBackoff(), "raised to one log flush")
	assert.Equal(t, 2*time.Second, parseMCP(t, map[string]string{"MCP_ENABLED": "true", "MCP_RETRY_SCHEDULE": "2,60", "LOG_BATCH_THRESHOLD_SECONDS": "0"}, "").GetRetryPollBackoff())
	assert.Equal(t, 15*time.Second, parseMCP(t, map[string]string{"MCP_ENABLED": "true", "MCP_RETRY_SCHEDULE": "15,60"}, "").GetRetryPollBackoff())
}
