package config_test

import (
	"bytes"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// The MCP settings are documented by hand in the configuration reference and
// the example files. These tests keep the names and defaults in step.

// envByYAMLPath maps every YAML key path ("mcp.ttl_min") of typ to its env
// var name.
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

func configEnvNames() (byYAML map[string]string, envs map[string]bool) {
	byYAML = map[string]string{}
	envByYAMLPath(reflect.TypeOf(config.Config{}), "", "", byYAML)
	envs = map[string]bool{}
	for _, env := range byYAML {
		envs[env] = true
	}
	return byYAML, envs
}

func isMCPEnv(name string) bool {
	return strings.HasPrefix(name, "MCP_") || strings.HasPrefix(name, "MAX_MCP_")
}

// requiredEnv is the minimum for Validate to pass.
var requiredEnv = map[string]string{
	"POSTGRES_URL":          "postgres://localhost:5432/outpost",
	"RABBITMQ_SERVER_URL":   "amqp://localhost:5672",
	"AES_ENCRYPTION_SECRET": "secret",
}

func TestMCPConfig_MatchesConfigurationReference(t *testing.T) {
	data, err := os.ReadFile("../../docs/content/self-hosting/configuration.mdoc")
	require.NoError(t, err)
	_, section, ok := strings.Cut(string(data), "\n## MCP Events\n")
	require.True(t, ok, "configuration.mdoc has an MCP Events section")
	section, _, _ = strings.Cut(section, "\n## ")

	byYAML, envs := configEnvNames()
	row := regexp.MustCompile("(?m)^\\| `([A-Z0-9_]+)` \\| `([a-z0-9_.]+)` \\| (?:`([^`]*)`|—) \\|")
	documented := map[string]bool{}
	defaults := parseMCP(t, nil, "")
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		env, yamlKey, def := m[1], m[2], m[3]
		documented[env] = true
		assert.Equal(t, env, byYAML[yamlKey], "YAML key %s", yamlKey)
		if def == "" {
			continue
		}
		// Setting the documented default changes nothing.
		cfg := parseMCP(t, map[string]string{env: def}, "")
		assert.Equal(t, defaults.MCP, cfg.MCP, "documented default of %s", env)
		assert.Equal(t, defaults.MaxMCPSubscriptionsPerTenant, cfg.MaxMCPSubscriptionsPerTenant, env)
		assert.Equal(t, defaults.MaxMCPSubscriptionsPerPrincipal, cfg.MaxMCPSubscriptionsPerPrincipal, env)
	}
	for env := range envs {
		if isMCPEnv(env) {
			assert.True(t, documented[env], "%s is documented in the MCP Events table", env)
		}
	}
	assert.True(t, envs["TOPICS_ALLOW_BREAKING_CHANGES"])
	assert.Contains(t, string(data), "| `TOPICS_ALLOW_BREAKING_CHANGES` | `false` |")
	assert.Contains(t, string(data), "| `PORTAL_SHOW_MCP_DESTINATIONS` | `true` |")
}

func TestMCPConfig_MatchesEnvExample(t *testing.T) {
	data, err := os.ReadFile("../../.env.example")
	require.NoError(t, err)

	_, envs := configEnvNames()
	line := regexp.MustCompile(`(?m)^# ((?:MCP|MAX_MCP)_[A-Z0-9_]+|TOPICS_ALLOW_BREAKING_CHANGES)="([^"]*)"$`)
	env := map[string]string{}
	for _, m := range line.FindAllStringSubmatch(string(data), -1) {
		assert.True(t, envs[m[1]], "%s in .env.example is a config env var", m[1])
		env[m[1]] = m[2]
	}
	for name := range envs {
		if isMCPEnv(name) {
			assert.Contains(t, env, name, "%s is in .env.example", name)
		}
	}
	for k, v := range requiredEnv {
		env[k] = v
	}

	m := &mockOS{files: map[string][]byte{}, envVars: env}
	cfg, err := config.ParseWithOS(config.Flags{}, m)
	require.NoError(t, err)
	want := defaultMCPConfig()
	want.ServerURL = env["MCP_SERVER_URL"]
	want.CallbackAllowlist = config.StringList{"localhost"}
	want.ProxyURL = env["MCP_PROXY_URL"]
	assert.Equal(t, want, cfg.MCP)
	assert.Equal(t, 100, cfg.MaxMCPSubscriptionsPerTenant)
	assert.Equal(t, 20, cfg.MaxMCPSubscriptionsPerPrincipal)
}

func TestMCPConfig_MatchesYAMLExample(t *testing.T) {
	data, err := os.ReadFile("../../.outpost.yaml.example")
	require.NoError(t, err)
	_, section, ok := strings.Cut(string(data), "\n## MCP Events\n")
	require.True(t, ok, ".outpost.yaml.example has an MCP Events section")
	section, _, _ = strings.Cut(section, "\n## ")

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

	m := &mockOS{
		files:   map[string][]byte{"config.yaml": []byte(doc.String())},
		envVars: map[string]string{"CONFIG": "config.yaml"},
	}
	for k, v := range requiredEnv {
		m.envVars[k] = v
	}
	cfg, err := config.ParseWithOS(config.Flags{}, m)
	require.NoError(t, err)
	want := defaultMCPConfig()
	want.ServerURL = "https://mcp.example.com/mcp"
	want.CallbackAllowlist = config.StringList{"localhost"}
	assert.Equal(t, want, cfg.MCP)
	assert.Equal(t, 100, cfg.MaxMCPSubscriptionsPerTenant)
	assert.Equal(t, 20, cfg.MaxMCPSubscriptionsPerPrincipal)
}
