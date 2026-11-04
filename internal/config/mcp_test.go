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
	"github.com/hookdeck/outpost/internal/mcpevents"
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

		SubscriptionTTLDefaultSeconds: 3600,
		SubscriptionTTLMinSeconds:     300,
		SubscriptionTTLMaxSeconds:     86400,
	}
}

func parseMCP(t *testing.T, env map[string]string, yamlDoc string) *config.Config {
	t.Helper()
	cfg, err := parseMCPErr(env, yamlDoc, nil)
	require.NoError(t, err)
	return cfg
}

func parseMCPErr(env map[string]string, yamlDoc string, files map[string]string) (*config.Config, error) {
	all := map[string]string{}
	for k, v := range requiredEnv {
		all[k] = v
	}
	for k, v := range env {
		all[k] = v
	}
	m := &mockOS{files: map[string][]byte{}, envVars: all}
	for name, content := range files {
		m.files[name] = []byte(content)
	}
	if yamlDoc != "" {
		m.files["config.yaml"] = []byte(yamlDoc)
		m.envVars["CONFIG"] = "config.yaml"
	}
	return config.ParseWithOS(config.Flags{}, m)
}

func TestMCPConfig_Defaults(t *testing.T) {
	cfg := parseMCP(t, nil, "")
	assert.Equal(t, defaultMCPConfig(), cfg.Experimental.MCP)
	assert.False(t, cfg.Experimental.MCP.Enabled)
	assert.Empty(t, cfg.MCPWarnings())
}

func TestMCPConfig_EnvAndYAML(t *testing.T) {
	cfg := parseMCP(t, map[string]string{
		"EXPERIMENTAL_MCP_ENABLED":                  "true",
		"EXPERIMENTAL_MCP_ALLOW_INSECURE_CALLBACKS": "true",
		"EXPERIMENTAL_MCP_RETRY_SCHEDULE":           "5,10",
		"EXPERIMENTAL_MCP_VERIFICATION_TTL_SECONDS": "5400",
	}, "")
	assert.Equal(t, config.MCPConfig{
		Enabled:                true,
		AllowInsecureCallbacks: true,
		RetrySchedule:          []int{5, 10},
		VerificationTTLSeconds: 5400,

		SubscriptionTTLDefaultSeconds: 3600,
		SubscriptionTTLMinSeconds:     300,
		SubscriptionTTLMaxSeconds:     86400,
	}, cfg.Experimental.MCP)

	cfg = parseMCP(t, nil, "experimental:\n  mcp:\n    enabled: true\n    retry_schedule: [7]\n    verification_ttl_seconds: 3600\n")
	assert.Equal(t, config.MCPConfig{
		Enabled:                true,
		RetrySchedule:          []int{7},
		VerificationTTLSeconds: 3600,

		SubscriptionTTLDefaultSeconds: 3600,
		SubscriptionTTLMinSeconds:     300,
		SubscriptionTTLMaxSeconds:     86400,
	}, cfg.Experimental.MCP)
}

func TestMCPConfig_Validation(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"zero schedule entry", map[string]string{"EXPERIMENTAL_MCP_RETRY_SCHEDULE": "30,0"}, "EXPERIMENTAL_MCP_RETRY_SCHEDULE"},
		{"negative schedule entry", map[string]string{"EXPERIMENTAL_MCP_RETRY_SCHEDULE": "-1"}, "EXPERIMENTAL_MCP_RETRY_SCHEDULE"},
		{"zero ttl", map[string]string{"EXPERIMENTAL_MCP_VERIFICATION_TTL_SECONDS": "0"}, "EXPERIMENTAL_MCP_VERIFICATION_TTL_SECONDS"},
		{"zero subscription ttl", map[string]string{"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MIN_SECONDS": "0"}, "EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MIN_SECONDS"},
		{"subscription min above default", map[string]string{"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MIN_SECONDS": "7200"}, "EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MAX_SECONDS"},
		{"subscription default above max", map[string]string{"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MAX_SECONDS": "60", "EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MIN_SECONDS": "10"}, "EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MAX_SECONDS"},
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
			all["EXPERIMENTAL_MCP_ENABLED"] = "true"
			_, err := config.ParseWithOS(config.Flags{}, &mockOS{files: map[string][]byte{}, envVars: all})
			require.Error(t, err)
			assert.ErrorIs(t, err, config.ErrInvalidMCPConfig)
			assert.Contains(t, err.Error(), tt.want)

			delete(all, "EXPERIMENTAL_MCP_ENABLED")
			_, err = config.ParseWithOS(config.Flags{}, &mockOS{files: map[string][]byte{}, envVars: all})
			assert.NoError(t, err, "keys are not validated while MCP is off")
		})
	}
}

func TestMCPConfig_SubscriptionTTL(t *testing.T) {
	assert.Nil(t, parseMCP(t, nil, "").MCPSubscriptionTTL())
	cfg := parseMCP(t, map[string]string{
		"EXPERIMENTAL_MCP_ENABLED":                          "true",
		"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_DEFAULT_SECONDS": "5",
		"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MIN_SECONDS":     "2",
		"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MAX_SECONDS":     "8",
	}, "")
	assert.Equal(t, &mcpevents.TTLConfig{Default: 5 * time.Second, Min: 2 * time.Second, Max: 8 * time.Second}, cfg.MCPSubscriptionTTL())
}

func TestMCPConfig_RetryBackoff(t *testing.T) {
	cfg := parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_RETRY_SCHEDULE": "5,15,60"}, "")
	b, limit := cfg.MCPRetryBackoff()
	assert.Equal(t, 3, limit)
	assert.Equal(t, time.Duration(cfg.LogBatchThresholdSeconds)*time.Second, b.Duration(0), "raised to one log flush")
	assert.Equal(t, 15*time.Second, b.Duration(1))
	assert.Equal(t, 60*time.Second, b.Duration(2))

	_, global := cfg.GetRetryBackoff()
	assert.Equal(t, cfg.RetryMaxLimit, global, "the global policy is unchanged")
}

// otherWarnings drops the warning about missing event definitions.
func otherWarnings(cfg *config.Config) []string {
	var out []string
	for _, w := range cfg.MCPWarnings() {
		if !strings.Contains(w, "defines no events") {
			out = append(out, w)
		}
	}
	return out
}

func TestMCPConfig_Warnings(t *testing.T) {
	cfg := parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST": "localhost", "EXPERIMENTAL_MCP_ALLOW_INSECURE_CALLBACKS": "true"}, "")
	warnings := strings.Join(cfg.MCPWarnings(), "\n")
	assert.Contains(t, warnings, "EXPERIMENTAL_MCP_ALLOW_INSECURE_CALLBACKS")
	assert.NotContains(t, warnings, "has no effect")

	cfg = parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_ALLOW_INSECURE_CALLBACKS": "true"}, "")
	assert.Contains(t, strings.Join(cfg.MCPWarnings(), "\n"), "has no effect")

	cfg = parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_RETRY_SCHEDULE": "1,5,60"}, "")
	assert.Equal(t, []string{"EXPERIMENTAL_MCP_RETRY_SCHEDULE: delays below LOG_BATCH_THRESHOLD_SECONDS (10s) are raised to it: 1s, 5s"}, otherWarnings(cfg))

	cfg = parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_RETRY_SCHEDULE": "1,5", "LOG_BATCH_THRESHOLD_SECONDS": "0"}, "")
	assert.Empty(t, otherWarnings(cfg))

	cfg = parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_ALLOW_INSECURE_CALLBACKS": "true"}, "")
	assert.Empty(t, cfg.MCPWarnings(), "nothing to warn about while MCP is off")
}

// The MCP settings are documented by hand in the configuration reference and
// the example files. These tests keep the names and defaults in step.

// envByYAMLPath maps every YAML key path ("experimental.mcp.retry_schedule") of typ to
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
		if strings.HasPrefix(env, "EXPERIMENTAL_MCP_") {
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
		assert.Equal(t, defaults.Experimental.MCP, cfg.Experimental.MCP, "documented default of %s", env)
	}
	for env := range mcpEnvNames() {
		assert.True(t, documented[env], "%s is documented in the MCP Events table", env)
	}
}

func TestMCPConfig_MatchesEnvExample(t *testing.T) {
	data, err := os.ReadFile("../../.env.example")
	require.NoError(t, err)

	line := regexp.MustCompile(`(?m)^# (EXPERIMENTAL_MCP_[A-Z0-9_]+)="([^"]*)"$`)
	env := map[string]string{}
	for _, m := range line.FindAllStringSubmatch(string(data), -1) {
		assert.Contains(t, mcpEnvNames(), m[1], "%s in .env.example is a config env var", m[1])
		env[m[1]] = m[2]
	}
	for name := range mcpEnvNames() {
		assert.Contains(t, env, name, "%s is in .env.example", name)
	}

	cfg, err := parseMCPErr(env, "", map[string]string{env["EXPERIMENTAL_MCP_EVENTS_SCHEMA"]: "events:\n  - name: user.created\n    inputSchema: {type: object, properties: {}}\n    payloadSchema: {type: object}\n"})
	require.NoError(t, err)
	want := defaultMCPConfig()
	want.Enabled = true
	require.NotNil(t, cfg.MCPCatalog())
	cfg.Experimental.MCP.EventsSchema = config.MCPEventsSchema{}
	assert.Equal(t, want, cfg.Experimental.MCP)
}

func TestMCPConfig_MatchesYAMLExample(t *testing.T) {
	data, err := os.ReadFile("../../.outpost.yaml.example")
	require.NoError(t, err)
	_, section, ok := strings.Cut(string(data), "\n## MCP Events\n")
	require.True(t, ok, ".outpost.yaml.example has an MCP Events section")
	section, _, _ = strings.Cut(section, "\n\n")

	// Uncomment the commented-out keys, leaving prose comments out.
	key := regexp.MustCompile(`^# (\s*(?:- )?[A-Za-z0-9_.]+:.*)$`)
	var doc strings.Builder
	for _, l := range strings.Split(section, "\n") {
		if m := key.FindStringSubmatch(l); m != nil {
			doc.WriteString(m[1] + "\n")
		}
	}
	require.Contains(t, doc.String(), "experimental:\n  mcp:\n")

	// Every key is a config key.
	dec := yaml.NewDecoder(bytes.NewReader([]byte(doc.String())))
	dec.KnownFields(true)
	var strict config.Config
	require.NoError(t, dec.Decode(&strict))

	cfg := parseMCP(t, nil, doc.String())
	require.NotNil(t, cfg.MCPCatalog())
	cfg.Experimental.MCP.EventsSchema = config.MCPEventsSchema{}
	want := defaultMCPConfig()
	want.Enabled = true
	assert.Equal(t, want, cfg.Experimental.MCP)
}

func TestMCPConfig_RetryPollBackoff(t *testing.T) {
	assert.Equal(t, 30*time.Second, parseMCP(t, nil, "").GetRetryPollBackoff())
	assert.Equal(t, 30*time.Second, parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_RETRY_SCHEDULE": "2"}, "").GetRetryPollBackoff(), "ignored while MCP is off")
	assert.Equal(t, 10*time.Second, parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_RETRY_SCHEDULE": "2,60"}, "").GetRetryPollBackoff(), "raised to one log flush")
	assert.Equal(t, 2*time.Second, parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_RETRY_SCHEDULE": "2,60", "LOG_BATCH_THRESHOLD_SECONDS": "0"}, "").GetRetryPollBackoff())
	assert.Equal(t, 15*time.Second, parseMCP(t, map[string]string{"EXPERIMENTAL_MCP_ENABLED": "true", "EXPERIMENTAL_MCP_RETRY_SCHEDULE": "15,60"}, "").GetRetryPollBackoff())
}
