package config_test

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

// parseMCP parses config from env vars and, when yamlConfig is set, a YAML
// config file.
func parseMCP(t *testing.T, env map[string]string, yamlConfig string) *config.Config {
	t.Helper()
	cfg, err := parseMCPErr(env, yamlConfig)
	require.NoError(t, err)
	return cfg
}

func parseMCPErr(env map[string]string, yamlConfig string) (*config.Config, error) {
	m := &mockOS{files: map[string][]byte{}, envVars: map[string]string{}}
	for k, v := range env {
		m.envVars[k] = v
	}
	if yamlConfig != "" {
		m.files["config.yaml"] = []byte(yamlConfig)
		m.envVars["CONFIG"] = "config.yaml"
	}
	return config.ParseWithoutValidation(config.Flags{}, m)
}

func defaultMCPConfig() config.MCPConfig {
	return config.MCPConfig{
		VerificationTTL:          config.Duration(24 * time.Hour),
		VerificationRateLimit:    10,
		VerificationFailureLimit: 120,
		SecretRotationGrace:      config.Duration(24 * time.Hour),
		TTLDefault:               config.Duration(time.Hour),
		TTLMin:                   config.Duration(5 * time.Minute),
		TTLMax:                   config.Duration(24 * time.Hour),
		RetrySchedule:            []int{30, 120, 600},
		ErrorCodes:               "sketch",
		SendTerminated:           true,
		ExpirySweepInterval:      config.Duration(30 * time.Second),
		MaxInFlightPerHost:       8,
	}
}

func TestMCPConfig_Defaults(t *testing.T) {
	cfg := parseMCP(t, nil, "")

	// Pinned to the documented defaults (configuration.mdoc, .env.example).
	assert.Equal(t, defaultMCPConfig(), cfg.MCP)
	assert.Equal(t, 100, cfg.MaxMCPSubscriptionsPerTenant)
	assert.Equal(t, 20, cfg.MaxMCPSubscriptionsPerPrincipal)
	assert.False(t, cfg.TopicsAllowBreakingChanges)
	assert.True(t, cfg.Portal.ShowMCPDestinations)

	// Defaults are valid.
	require.NoError(t, validConfig().Validate(config.Flags{}))
}

func TestMCPConfig_Env(t *testing.T) {
	cfg := parseMCP(t, map[string]string{
		"MCP_SERVER_URL":                      "https://mcp.example.com/mcp",
		"MCP_CALLBACK_ALLOWLIST":              "localhost, 10.0.0.0/8,,",
		"MCP_ALLOW_INSECURE_CALLBACKS":        "true",
		"MCP_PROXY_URL":                       "http://proxy:3128",
		"MCP_VERIFICATION_TTL":                "3600",
		"MCP_VERIFICATION_RATE_LIMIT":         "0",
		"MCP_VERIFICATION_FAILURE_LIMIT":      "50",
		"MCP_SECRET_ROTATION_GRACE":           "2h",
		"MCP_TTL_DEFAULT":                     "600",
		"MCP_TTL_MIN":                         "1s",
		"MCP_TTL_MAX":                         "48h",
		"MCP_ALLOW_NO_EXPIRY":                 "true",
		"MCP_RETRY_SCHEDULE":                  "1,1,1",
		"MCP_ERROR_CODES":                     "sep-3415",
		"MCP_SEND_TERMINATED":                 "false",
		"MCP_EXPIRY_SWEEP_INTERVAL":           "1",
		"MCP_MAX_INFLIGHT_PER_HOST":           "0",
		"MAX_MCP_SUBSCRIPTIONS_PER_TENANT":    "500",
		"MAX_MCP_SUBSCRIPTIONS_PER_PRINCIPAL": "0",
		"TOPICS_ALLOW_BREAKING_CHANGES":       "true",
	}, "")

	assert.Equal(t, config.MCPConfig{
		ServerURL:                "https://mcp.example.com/mcp",
		CallbackAllowlist:        config.StringList{"localhost", "10.0.0.0/8"},
		AllowInsecureCallbacks:   true,
		ProxyURL:                 "http://proxy:3128",
		VerificationTTL:          config.Duration(time.Hour),
		VerificationRateLimit:    0,
		VerificationFailureLimit: 50,
		SecretRotationGrace:      config.Duration(2 * time.Hour),
		TTLDefault:               config.Duration(10 * time.Minute),
		TTLMin:                   config.Duration(time.Second),
		TTLMax:                   config.Duration(48 * time.Hour),
		AllowNoExpiry:            true,
		RetrySchedule:            []int{1, 1, 1},
		ErrorCodes:               "sep-3415",
		SendTerminated:           false,
		ExpirySweepInterval:      config.Duration(time.Second),
		MaxInFlightPerHost:       0,
	}, cfg.MCP)
	assert.Equal(t, 500, cfg.MaxMCPSubscriptionsPerTenant)
	assert.Equal(t, 0, cfg.MaxMCPSubscriptionsPerPrincipal)
	assert.True(t, cfg.TopicsAllowBreakingChanges)
}

func TestMCPConfig_YAML(t *testing.T) {
	cfg := parseMCP(t, nil, `
topics_allow_breaking_changes: true
mcp:
  server_url: "https://mcp.example.com/mcp"
  callback_allowlist: [localhost, 192.168.0.0/16]
  allow_insecure_callbacks: true
  proxy_url: "socks5://proxy:1080"
  verification_ttl: 7200
  verification_rate_limit: 5
  verification_failure_limit: 0
  secret_rotation_grace: "30m"
  ttl_default: 2h
  ttl_min: "60"
  ttl_max: 86400
  allow_no_expiry: true
  retry_schedule: [5, 10]
  error_codes: sep-3415
  send_terminated: false
  expiry_sweep_interval: 10s
  max_inflight_per_host: 2
max_mcp_subscriptions_per_tenant: 10000
max_mcp_subscriptions_per_principal: 3
`)

	assert.Equal(t, config.MCPConfig{
		ServerURL:                "https://mcp.example.com/mcp",
		CallbackAllowlist:        config.StringList{"localhost", "192.168.0.0/16"},
		AllowInsecureCallbacks:   true,
		ProxyURL:                 "socks5://proxy:1080",
		VerificationTTL:          config.Duration(2 * time.Hour),
		VerificationRateLimit:    5,
		VerificationFailureLimit: 0,
		SecretRotationGrace:      config.Duration(30 * time.Minute),
		TTLDefault:               config.Duration(2 * time.Hour),
		TTLMin:                   config.Duration(time.Minute),
		TTLMax:                   config.Duration(24 * time.Hour),
		AllowNoExpiry:            true,
		RetrySchedule:            []int{5, 10},
		ErrorCodes:               "sep-3415",
		SendTerminated:           false,
		ExpirySweepInterval:      config.Duration(10 * time.Second),
		MaxInFlightPerHost:       2,
	}, cfg.MCP)
	assert.Equal(t, 10000, cfg.MaxMCPSubscriptionsPerTenant)
	assert.Equal(t, 3, cfg.MaxMCPSubscriptionsPerPrincipal)
	assert.True(t, cfg.TopicsAllowBreakingChanges)
}

func TestMCPConfig_YAMLPartialKeepsDefaults(t *testing.T) {
	cfg := parseMCP(t, nil, "mcp:\n  ttl_default: 30m\n  callback_allowlist: localhost\n")

	want := defaultMCPConfig()
	want.TTLDefault = config.Duration(30 * time.Minute)
	want.CallbackAllowlist = config.StringList{"localhost"}
	assert.Equal(t, want, cfg.MCP)
	assert.Equal(t, 100, cfg.MaxMCPSubscriptionsPerTenant)
}

func TestMCPConfig_EnvOverridesYAML(t *testing.T) {
	cfg := parseMCP(t, map[string]string{
		"MCP_TTL_DEFAULT":                  "45m",
		"MCP_RETRY_SCHEDULE":               "2,4",
		"MCP_CALLBACK_ALLOWLIST":           "::1",
		"MCP_SEND_TERMINATED":              "true",
		"MAX_MCP_SUBSCRIPTIONS_PER_TENANT": "7",
		// caarlos0/env ignores a present-but-empty variable: YAML wins.
		"MCP_ERROR_CODES": "",
	}, `
mcp:
  ttl_default: 2h
  retry_schedule: [10]
  callback_allowlist: [localhost]
  send_terminated: false
  error_codes: sep-3415
max_mcp_subscriptions_per_tenant: 50
`)

	assert.Equal(t, config.Duration(45*time.Minute), cfg.MCP.TTLDefault)
	assert.Equal(t, []int{2, 4}, cfg.MCP.RetrySchedule)
	assert.Equal(t, config.StringList{"::1"}, cfg.MCP.CallbackAllowlist)
	assert.True(t, cfg.MCP.SendTerminated)
	assert.Equal(t, 7, cfg.MaxMCPSubscriptionsPerTenant)
	assert.Equal(t, "sep-3415", cfg.MCP.ErrorCodes)
}

func TestMCPConfig_ParseErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		yaml string
		want string
	}{
		{name: "env duration", env: map[string]string{"MCP_TTL_DEFAULT": "1d"}, want: `invalid duration "1d"`},
		// caarlos0/env names the struct field, as for RETRY_SCHEDULE.
		{name: "env retry schedule", env: map[string]string{"MCP_RETRY_SCHEDULE": "30,soon"}, want: `field "RetrySchedule"`},
		{name: "env limit", env: map[string]string{"MAX_MCP_SUBSCRIPTIONS_PER_TENANT": "many"}, want: `field "MaxMCPSubscriptionsPerTenant"`},
		{name: "yaml duration", yaml: "mcp:\n  ttl_max: forever\n", want: `invalid duration "forever"`},
		{name: "yaml allowlist mapping", yaml: "mcp:\n  callback_allowlist:\n    a: b\n", want: "a list must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMCPErr(tt.env, tt.yaml)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestMCPConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *config.Config)
		wantErr string // "" = valid
	}{
		// TTLs
		{name: "ttls equal", mutate: func(c *config.Config) {
			c.MCP.TTLMin, c.MCP.TTLDefault, c.MCP.TTLMax = config.Duration(time.Hour), config.Duration(time.Hour), config.Duration(time.Hour)
		}},
		{name: "ttls of one second", mutate: func(c *config.Config) {
			c.MCP.TTLMin, c.MCP.TTLDefault = config.Duration(time.Second), config.Duration(time.Second)
		}},
		{name: "ttl min zero", mutate: func(c *config.Config) { c.MCP.TTLMin = 0 }, wantErr: "MCP_TTL_MIN (mcp.ttl_min) must be at least 1s, got 0s"},
		{name: "ttl min negative", mutate: func(c *config.Config) { c.MCP.TTLMin = config.Duration(-time.Minute) }, wantErr: "MCP_TTL_MIN"},
		{name: "ttl min under a second", mutate: func(c *config.Config) { c.MCP.TTLMin = config.Duration(500 * time.Millisecond) }, wantErr: "MCP_TTL_MIN (mcp.ttl_min) must be at least 1s"},
		{name: "ttl default zero", mutate: func(c *config.Config) { c.MCP.TTLDefault = 0 }, wantErr: "MCP_TTL_DEFAULT (mcp.ttl_default) must be at least 1s"},
		{name: "ttl max zero", mutate: func(c *config.Config) { c.MCP.TTLMax = 0 }, wantErr: "MCP_TTL_MAX (mcp.ttl_max) must be at least 1s"},
		{name: "ttl min above default", mutate: func(c *config.Config) { c.MCP.TTLMin = config.Duration(2 * time.Hour) }, wantErr: "MCP_TTL_MIN (2h0m0s), MCP_TTL_DEFAULT (1h0m0s) and MCP_TTL_MAX (24h0m0s) must be in increasing order or equal"},
		{name: "ttl default above max", mutate: func(c *config.Config) { c.MCP.TTLDefault = config.Duration(25 * time.Hour) }, wantErr: "must be in increasing order or equal"},
		{name: "ttl max below min", mutate: func(c *config.Config) { c.MCP.TTLMax = config.Duration(time.Minute) }, wantErr: "must be in increasing order or equal"},

		// Other durations
		{name: "verification ttl zero", mutate: func(c *config.Config) { c.MCP.VerificationTTL = 0 }, wantErr: "MCP_VERIFICATION_TTL (mcp.verification_ttl) must be greater than 0"},
		{name: "verification ttl negative", mutate: func(c *config.Config) { c.MCP.VerificationTTL = config.Duration(-time.Second) }, wantErr: "MCP_VERIFICATION_TTL"},
		{name: "rotation grace zero", mutate: func(c *config.Config) { c.MCP.SecretRotationGrace = 0 }, wantErr: "MCP_SECRET_ROTATION_GRACE (mcp.secret_rotation_grace) must be greater than 0"},
		{name: "sweep interval one second", mutate: func(c *config.Config) { c.MCP.ExpirySweepInterval = config.Duration(time.Second) }},
		{name: "sweep interval under a second", mutate: func(c *config.Config) { c.MCP.ExpirySweepInterval = config.Duration(999 * time.Millisecond) }, wantErr: "MCP_EXPIRY_SWEEP_INTERVAL (mcp.expiry_sweep_interval) must be at least 1s"},

		// Limits
		{name: "rate limit zero turns it off", mutate: func(c *config.Config) { c.MCP.VerificationRateLimit = 0 }},
		{name: "rate limit negative", mutate: func(c *config.Config) { c.MCP.VerificationRateLimit = -1 }, wantErr: "MCP_VERIFICATION_RATE_LIMIT (mcp.verification_rate_limit) must be 0 (no limit) or more, got -1"},
		{name: "failure limit zero turns it off", mutate: func(c *config.Config) { c.MCP.VerificationFailureLimit = 0 }},
		{name: "failure limit negative", mutate: func(c *config.Config) { c.MCP.VerificationFailureLimit = -1 }, wantErr: "MCP_VERIFICATION_FAILURE_LIMIT"},
		{name: "inflight zero is unlimited", mutate: func(c *config.Config) { c.MCP.MaxInFlightPerHost = 0 }},
		{name: "inflight negative", mutate: func(c *config.Config) { c.MCP.MaxInFlightPerHost = -1 }, wantErr: "MCP_MAX_INFLIGHT_PER_HOST (mcp.max_inflight_per_host) must be 0 (no limit) or more"},
		{name: "tenant limit 1", mutate: func(c *config.Config) { c.MaxMCPSubscriptionsPerTenant = 1 }},
		{name: "tenant limit 10000", mutate: func(c *config.Config) { c.MaxMCPSubscriptionsPerTenant = 10000 }},
		{name: "tenant limit 0", mutate: func(c *config.Config) { c.MaxMCPSubscriptionsPerTenant = 0 }, wantErr: "MAX_MCP_SUBSCRIPTIONS_PER_TENANT (max_mcp_subscriptions_per_tenant) must be between 1 and 10000, got 0"},
		{name: "tenant limit 10001", mutate: func(c *config.Config) { c.MaxMCPSubscriptionsPerTenant = 10001 }, wantErr: "must be between 1 and 10000, got 10001"},
		{name: "principal limit 0 turns it off", mutate: func(c *config.Config) { c.MaxMCPSubscriptionsPerPrincipal = 0 }},
		{name: "principal limit above tenant limit", mutate: func(c *config.Config) { c.MaxMCPSubscriptionsPerPrincipal = 1000 }},
		{name: "principal limit negative", mutate: func(c *config.Config) { c.MaxMCPSubscriptionsPerPrincipal = -1 }, wantErr: "MAX_MCP_SUBSCRIPTIONS_PER_PRINCIPAL (max_mcp_subscriptions_per_principal) must be 0 (no limit) or more"},

		// Retry schedule
		{name: "retry schedule of one", mutate: func(c *config.Config) { c.MCP.RetrySchedule = []int{1} }},
		{name: "retry schedule empty", mutate: func(c *config.Config) { c.MCP.RetrySchedule = []int{} }, wantErr: "MCP_RETRY_SCHEDULE (mcp.retry_schedule) must list at least one delay"},
		{name: "retry schedule zero entry", mutate: func(c *config.Config) { c.MCP.RetrySchedule = []int{30, 0} }, wantErr: "MCP_RETRY_SCHEDULE (mcp.retry_schedule) entries must be at least 1 second, got 0 at index 1"},
		{name: "retry schedule negative entry", mutate: func(c *config.Config) { c.MCP.RetrySchedule = []int{-5} }, wantErr: "got -5 at index 0"},
		{name: "retry schedule overflowing entry", mutate: func(c *config.Config) { c.MCP.RetrySchedule = []int{1 << 62} }, wantErr: "is too large"},

		// Error codes
		{name: "error codes sep-3415", mutate: func(c *config.Config) { c.MCP.ErrorCodes = "sep-3415" }},
		{name: "error codes any case", mutate: func(c *config.Config) { c.MCP.ErrorCodes = " SEP-3415 " }},
		{name: "error codes empty is sketch", mutate: func(c *config.Config) { c.MCP.ErrorCodes = "" }},
		{name: "error codes unknown", mutate: func(c *config.Config) { c.MCP.ErrorCodes = "jsonrpc" }, wantErr: `MCP_ERROR_CODES (mcp.error_codes) must be "sketch" or "sep-3415", got "jsonrpc"`},

		// Server URL
		{name: "server url https", mutate: func(c *config.Config) { c.MCP.ServerURL = "https://mcp.example.com/mcp" }},
		{name: "server url http with port", mutate: func(c *config.Config) { c.MCP.ServerURL = "http://localhost:8080/mcp?x=1" }},
		{name: "server url uppercase scheme", mutate: func(c *config.Config) { c.MCP.ServerURL = "HTTPS://mcp.example.com" }},
		{name: "server url relative", mutate: func(c *config.Config) { c.MCP.ServerURL = "/mcp" }, wantErr: "MCP_SERVER_URL (mcp.server_url) must be an absolute http or https URL"},
		{name: "server url no scheme", mutate: func(c *config.Config) { c.MCP.ServerURL = "mcp.example.com/mcp" }, wantErr: "must be an absolute http or https URL"},
		{name: "server url other scheme", mutate: func(c *config.Config) { c.MCP.ServerURL = "ftp://mcp.example.com" }, wantErr: "must be an absolute http or https URL"},
		{name: "server url opaque", mutate: func(c *config.Config) { c.MCP.ServerURL = "https:mcp.example.com" }, wantErr: "must be an absolute http or https URL"},
		{name: "server url no host", mutate: func(c *config.Config) { c.MCP.ServerURL = "https:///mcp" }, wantErr: "must be an absolute http or https URL"},
		{name: "server url unparsable", mutate: func(c *config.Config) { c.MCP.ServerURL = "https://[::1/mcp" }, wantErr: "must be an absolute http or https URL"},
		{name: "server url credentials", mutate: func(c *config.Config) { c.MCP.ServerURL = "https://user:hunter2@mcp.example.com" }, wantErr: "must not contain credentials"},
		{name: "server url whitespace", mutate: func(c *config.Config) { c.MCP.ServerURL = "https://mcp.example.com/m cp" }, wantErr: "must not contain whitespace"},
		{name: "server url newline", mutate: func(c *config.Config) { c.MCP.ServerURL = "https://mcp.example.com/\n" }, wantErr: "must not contain whitespace"},

		// Proxy URL
		{name: "proxy http", mutate: func(c *config.Config) { c.MCP.ProxyURL = "http://user:pass@proxy:3128" }},
		{name: "proxy https", mutate: func(c *config.Config) { c.MCP.ProxyURL = "https://proxy" }},
		{name: "proxy socks5h", mutate: func(c *config.Config) { c.MCP.ProxyURL = "socks5h://proxy:1080" }},
		{name: "proxy unsupported scheme", mutate: func(c *config.Config) { c.MCP.ProxyURL = "ftp://proxy" }, wantErr: "MCP_PROXY_URL (mcp.proxy_url) scheme must be one of http, https, socks5, socks5h"},
		{name: "proxy without scheme", mutate: func(c *config.Config) { c.MCP.ProxyURL = "proxy:3128" }, wantErr: "scheme must be one of"},
		{name: "proxy without host", mutate: func(c *config.Config) { c.MCP.ProxyURL = "http://" }, wantErr: "MCP_PROXY_URL (mcp.proxy_url) has no host"},
		{name: "proxy chain", mutate: func(c *config.Config) { c.MCP.ProxyURL = "http://a:3128 http://b:3128" }, wantErr: "must be a single proxy URL without whitespace"},
		{name: "proxy unparsable", mutate: func(c *config.Config) { c.MCP.ProxyURL = "http://user:pa%zzss@proxy" }, wantErr: "MCP_PROXY_URL (mcp.proxy_url) is not a valid URL"},

		// Allowlist
		{name: "allowlist entries", mutate: func(c *config.Config) {
			c.MCP.CallbackAllowlist = config.StringList{"localhost", "10.0.0.0/8", "203.0.113.7", "fd00::/8"}
		}},
		{name: "allowlist host name", mutate: func(c *config.Config) { c.MCP.CallbackAllowlist = config.StringList{"example.com"} }, wantErr: `MCP_CALLBACK_ALLOWLIST (mcp.callback_allowlist): allowlist entry "example.com"`},
		{name: "allowlist wildcard", mutate: func(c *config.Config) { c.MCP.CallbackAllowlist = config.StringList{"*.corp"} }, wantErr: "MCP_CALLBACK_ALLOWLIST"},
		{name: "allowlist /0", mutate: func(c *config.Config) { c.MCP.CallbackAllowlist = config.StringList{"0.0.0.0/0"} }, wantErr: "a /0 range would disable the guard"},
		{name: "allowlist reports every bad entry", mutate: func(c *config.Config) { c.MCP.CallbackAllowlist = config.StringList{"a.com", "localhost", "::/0"} }, wantErr: `"a.com"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(c)
			err := c.Validate(config.Flags{})
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, config.ErrInvalidMCPConfig)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestMCPConfig_ValidateNeverLeaksProxyCredentials(t *testing.T) {
	for _, proxy := range []string{
		"http://user:hunter2@proxy:3128 http://other:hunter2@proxy2",
		"http://user:hunter2%zz@proxy",
		"http://user:hunter2@proxy:port",
		"ftp://user:hunter2@proxy",
		"http://user:hunter2@",
	} {
		c := validConfig()
		c.MCP.ProxyURL = proxy
		err := c.Validate(config.Flags{})
		require.ErrorIs(t, err, config.ErrInvalidMCPConfig, proxy)
		assert.NotContains(t, err.Error(), "hunter2", proxy)

		_, err = c.MCPProxyURL()
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "hunter2", proxy)
	}
}

func TestMCPConfig_ValidateThroughParse(t *testing.T) {
	_, err := config.ParseWithOS(config.Flags{}, &mockOS{
		files: map[string][]byte{},
		envVars: map[string]string{
			"POSTGRES_URL":          "postgres://localhost:5432/outpost",
			"RABBITMQ_SERVER_URL":   "amqp://localhost:5672",
			"AES_ENCRYPTION_SECRET": "secret",
			"MCP_TTL_MIN":           "2h",
		},
	})
	require.ErrorIs(t, err, config.ErrInvalidMCPConfig)
	assert.Contains(t, err.Error(), "must be in increasing order or equal")
}

func TestMCPConfig_RetryBackoff(t *testing.T) {
	c := validConfig()
	b, maxLimit := c.MCPRetryBackoff()
	assert.Equal(t, 3, maxLimit)
	// ±20% jitter, so attempts that failed together (a throttled burst to
	// one host) don't all retry together.
	require.IsType(t, &backoff.JitteredBackoff{}, b)
	jittered := b.(*backoff.JitteredBackoff)
	assert.Equal(t, &backoff.ScheduledBackoff{Schedule: []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}}, jittered.Backoff)
	assert.Equal(t, 0.2, jittered.Jitter)
	for range 100 {
		assert.InDelta(t, float64(30*time.Second), float64(b.Duration(0)), float64(6*time.Second))
		assert.InDelta(t, float64(10*time.Minute), float64(b.Duration(2)), float64(2*time.Minute))
	}

	c.MCP.RetrySchedule = []int{1, 1, 1, 1, 1}
	b, maxLimit = c.MCPRetryBackoff()
	assert.Equal(t, 5, maxLimit)
	assert.InDelta(t, float64(time.Second), float64(b.Duration(4)), float64(200*time.Millisecond))

	// The MCP schedule is separate from the global retry settings.
	c.RetrySchedule = []int{5, 10}
	require.NoError(t, c.Validate(config.Flags{}))
	_, maxLimit = c.MCPRetryBackoff()
	assert.Equal(t, 5, maxLimit)
	_, globalMax := c.GetRetryBackoff()
	assert.Equal(t, 2, globalMax)
}

func TestMCPConfig_TTLConfig(t *testing.T) {
	c := validConfig()
	assert.Equal(t, mcpevents.TTLConfig{Default: time.Hour, Min: 5 * time.Minute, Max: 24 * time.Hour}, c.MCPTTLConfig())

	c.MCP.TTLDefault = config.Duration(10 * time.Minute)
	c.MCP.AllowNoExpiry = true
	assert.Equal(t, mcpevents.TTLConfig{Default: 10 * time.Minute, Min: 5 * time.Minute, Max: 24 * time.Hour, AllowNoExpiry: true}, c.MCPTTLConfig())
}

func TestMCPConfig_Allowlist(t *testing.T) {
	c := validConfig()
	allowlist, warnings, err := c.MCPAllowlist()
	require.NoError(t, err)
	require.NotNil(t, allowlist)
	assert.Empty(t, allowlist.Prefixes())
	assert.Empty(t, warnings)
	assert.False(t, allowlist.Contains(netip.MustParseAddr("127.0.0.1")))

	c.MCP.CallbackAllowlist = config.StringList{"localhost", "203.0.113.0/24"}
	allowlist, warnings, err = c.MCPAllowlist()
	require.NoError(t, err)
	assert.True(t, allowlist.Contains(netip.MustParseAddr("127.0.0.1")))
	assert.True(t, allowlist.Contains(netip.MustParseAddr("::1")))
	assert.True(t, allowlist.Contains(netip.MustParseAddr("203.0.113.9")))
	assert.False(t, allowlist.Contains(netip.MustParseAddr("10.0.0.1")))
	require.NotEmpty(t, warnings)
	assert.Contains(t, strings.Join(warnings, "\n"), `"localhost"`)

	c.MCP.CallbackAllowlist = config.StringList{"intranet.local"}
	allowlist, warnings, err = c.MCPAllowlist()
	require.ErrorIs(t, err, config.ErrInvalidMCPConfig)
	assert.Nil(t, allowlist)
	assert.Nil(t, warnings)
}

func TestMCPConfig_ProxyURL(t *testing.T) {
	c := validConfig()
	u, err := c.MCPProxyURL()
	require.NoError(t, err)
	assert.Nil(t, u, "unset")

	c.MCP.ProxyURL = "HTTP://user:pass@proxy.internal:3128"
	u, err = c.MCPProxyURL()
	require.NoError(t, err)
	assert.Equal(t, "http", u.Scheme)
	assert.Equal(t, "proxy.internal:3128", u.Host)
	pass, _ := u.User.Password()
	assert.Equal(t, "pass", pass)
}

func TestMCPConfig_CodeProfile(t *testing.T) {
	c := validConfig()
	assert.Equal(t, mcpevents.CodeProfileSketch, c.MCPCodeProfile())
	c.MCP.ErrorCodes = "sep-3415"
	assert.Equal(t, mcpevents.CodeProfileSEP3415, c.MCPCodeProfile())
	c.MCP.ErrorCodes = "Sep-3415 "
	assert.Equal(t, mcpevents.CodeProfileSEP3415, c.MCPCodeProfile())
	c.MCP.ErrorCodes = ""
	assert.Equal(t, mcpevents.CodeProfileSketch, c.MCPCodeProfile())
	c.MCP.ErrorCodes = "bogus" // rejected by Validate
	assert.Equal(t, mcpevents.CodeProfileSketch, c.MCPCodeProfile())
}

func TestMCPConfig_VerificationLimits(t *testing.T) {
	c := validConfig()
	rate, failure := c.MCPVerificationLimits()
	assert.Equal(t, 10, rate)
	assert.Equal(t, 120, failure)

	// 0 means no limit in config; mcpevents.VerifierConfig spells that -1
	// (its 0 means the default).
	c.MCP.VerificationRateLimit, c.MCP.VerificationFailureLimit = 0, 0
	rate, failure = c.MCPVerificationLimits()
	assert.Equal(t, -1, rate)
	assert.Equal(t, -1, failure)
}

func mcpCatalog(t *testing.T, defs string) *topicschema.Catalog {
	t.Helper()
	parsed, err := topicschema.ParseDefinitionsJSON([]byte(defs))
	require.NoError(t, err)
	catalog, err := topicschema.NewCatalog([]string{"order.created", "order.updated"}, parsed)
	require.NoError(t, err)
	return catalog
}

func TestMCPConfig_Warnings(t *testing.T) {
	mcpTopics := mcpCatalog(t, `{"order.created":{"mcp":{"enabled":true},"payload_schema":{"type":"object"}}}`)
	noMCPTopics := mcpCatalog(t, `{"order.created":{"validation":"warn","payload_schema":{"type":"object"}}}`)
	const concurrency = "DELIVERY_MAX_CONCURRENCY is 1 with MCP-enabled topics"

	t.Run("defaults without catalog", func(t *testing.T) {
		assert.Empty(t, validConfig().MCPWarnings(nil))
	})

	t.Run("low delivery concurrency with MCP topics", func(t *testing.T) {
		c := validConfig()
		warnings := c.MCPWarnings(mcpTopics)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], concurrency)
		assert.Contains(t, warnings[0], "at least 8")

		c.DeliveryMaxConcurrency = 7
		assert.Len(t, c.MCPWarnings(mcpTopics), 1)
		c.DeliveryMaxConcurrency = 8
		assert.Empty(t, c.MCPWarnings(mcpTopics))
	})

	t.Run("low delivery concurrency without MCP topics", func(t *testing.T) {
		c := validConfig()
		assert.Empty(t, c.MCPWarnings(noMCPTopics))
		assert.Empty(t, c.MCPWarnings(topicschema.EmptyCatalog([]string{"order.created"})))
		assert.Empty(t, c.MCPWarnings(nil))
	})

	t.Run("allowlist", func(t *testing.T) {
		c := validConfig()
		c.DeliveryMaxConcurrency = 8
		c.MCP.CallbackAllowlist = config.StringList{"localhost", "10.0.0.0/8", "8.8.8.8"}
		warnings := c.MCPWarnings(mcpTopics)
		// localhost opens two ranges (127.0.0.0/8, ::1/128); 8.8.8.8 is global.
		require.Len(t, warnings, 3)
		for _, w := range warnings {
			assert.True(t, strings.HasPrefix(w, "MCP_CALLBACK_ALLOWLIST: "), w)
		}
		assert.Contains(t, warnings[0], `"localhost"`)
		assert.Contains(t, warnings[1], `"localhost"`)
		assert.Contains(t, warnings[2], `"10.0.0.0/8"`)
	})

	t.Run("insecure callbacks", func(t *testing.T) {
		c := validConfig()
		c.MCP.AllowInsecureCallbacks = true
		warnings := c.MCPWarnings(nil)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "MCP_ALLOW_INSECURE_CALLBACKS is true but MCP_CALLBACK_ALLOWLIST is empty")

		c.MCP.CallbackAllowlist = config.StringList{"203.0.113.0/24"}
		warnings = c.MCPWarnings(nil)
		require.Len(t, warnings, 2) // the documentation range is non-global
		assert.Contains(t, warnings[1], "MCP_ALLOW_INSECURE_CALLBACKS is true: plain http callback URLs are accepted")
	})

	t.Run("everything", func(t *testing.T) {
		c := validConfig()
		c.MCP.AllowInsecureCallbacks = true
		c.MCP.CallbackAllowlist = config.StringList{"127.0.0.1"}
		warnings := c.MCPWarnings(mcpTopics)
		require.Len(t, warnings, 3)
		assert.Contains(t, warnings[0], concurrency)
		assert.Contains(t, warnings[1], "MCP_CALLBACK_ALLOWLIST: ")
		assert.Contains(t, warnings[2], "MCP_ALLOW_INSECURE_CALLBACKS")
	})
}

func TestTopicsAllowBreakingChanges(t *testing.T) {
	const warning = "TOPICS_ALLOW_BREAKING_CHANGES is set"
	hasWarning := func(c *config.Config) bool {
		for _, w := range c.DeprecationWarnings() {
			if strings.Contains(w, warning) {
				return true
			}
		}
		return false
	}

	cfg := parseMCP(t, nil, "")
	assert.False(t, cfg.TopicsAllowBreakingChanges)
	assert.False(t, hasWarning(cfg))

	cfg = parseMCP(t, map[string]string{"TOPICS_ALLOW_BREAKING_CHANGES": "true"}, "")
	assert.True(t, cfg.TopicsAllowBreakingChanges)
	assert.True(t, hasWarning(cfg))

	cfg = parseMCP(t, nil, "topics_allow_breaking_changes: true\n")
	assert.True(t, cfg.TopicsAllowBreakingChanges)
	assert.True(t, hasWarning(cfg))

	cfg = parseMCP(t, map[string]string{"TOPICS_ALLOW_BREAKING_CHANGES": "false"}, "topics_allow_breaking_changes: true\n")
	assert.False(t, cfg.TopicsAllowBreakingChanges)
	assert.False(t, hasWarning(cfg))

	// Other deprecation warnings are kept alongside it.
	cfg = parseMCP(t, map[string]string{
		"TOPICS_ALLOW_BREAKING_CHANGES":                        "true",
		"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_EVENT_ID_HEADER": "true",
	}, "")
	assert.True(t, hasWarning(cfg))
	assert.Len(t, cfg.DeprecationWarnings(), 2)
}

func TestGetRetryPollBackoff_MCPRetrySchedule(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *config.Config)
		want   time.Duration
	}{
		{name: "default MCP schedule stays at the 30s ceiling", mutate: func(c *config.Config) {}, want: 30 * time.Second},
		{name: "shorter MCP schedule wins", mutate: func(c *config.Config) { c.MCP.RetrySchedule = []int{10, 2, 60} }, want: 2 * time.Second},
		{name: "MCP schedule of one second", mutate: func(c *config.Config) { c.MCP.RetrySchedule = []int{1, 1, 1} }, want: time.Second},
		{name: "shorter global interval wins", mutate: func(c *config.Config) {
			c.RetryIntervalSeconds = 3
			c.MCP.RetrySchedule = []int{10}
		}, want: 3 * time.Second},
		{name: "shorter global schedule wins", mutate: func(c *config.Config) {
			c.RetrySchedule = []int{4}
			c.MCP.RetrySchedule = []int{5}
		}, want: 4 * time.Second},
		{name: "MCP schedule shorter than global schedule", mutate: func(c *config.Config) {
			c.RetrySchedule = []int{20}
			c.MCP.RetrySchedule = []int{5}
		}, want: 5 * time.Second},
		{name: "MCP schedule when the global interval is unset", mutate: func(c *config.Config) {
			c.RetryIntervalSeconds = 0
			c.MCP.RetrySchedule = []int{7}
		}, want: 7 * time.Second},
		{name: "explicit backoff is honored as-is", mutate: func(c *config.Config) {
			c.RetryPollBackoffMs = 10000
			c.MCP.RetrySchedule = []int{1}
		}, want: 10 * time.Second},
		{name: "unvalidated non-positive MCP entries are ignored", mutate: func(c *config.Config) {
			c.RetryIntervalSeconds = 6
			c.MCP.RetrySchedule = []int{0, -3}
		}, want: 6 * time.Second},
		{name: "empty MCP schedule is ignored", mutate: func(c *config.Config) {
			c.RetryIntervalSeconds = 6
			c.MCP.RetrySchedule = nil
		}, want: 6 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(c)
			assert.Equal(t, tt.want, c.GetRetryPollBackoff())
		})
	}
}

// summaryValues encodes LogConfigurationSummary into plain values: strings,
// int64, bool and []any for lists.
func summaryValues(t *testing.T, c *config.Config) map[string]any {
	t.Helper()
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range c.LogConfigurationSummary() {
		f.AddTo(enc)
	}
	return enc.Fields
}

func TestMCPConfig_LogConfigurationSummary(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		got := summaryValues(t, validConfig())
		want := map[string]any{
			"topics_allow_breaking_changes":       false,
			"mcp_server_url":                      "",
			"mcp_callback_allowlist":              []any{},
			"mcp_allow_insecure_callbacks":        false,
			"mcp_proxy_url":                       "",
			"mcp_verification_ttl":                "24h0m0s",
			"mcp_verification_rate_limit":         int64(10),
			"mcp_verification_failure_limit":      int64(120),
			"mcp_secret_rotation_grace":           "24h0m0s",
			"mcp_ttl_default":                     "1h0m0s",
			"mcp_ttl_min":                         "5m0s",
			"mcp_ttl_max":                         "24h0m0s",
			"mcp_allow_no_expiry":                 false,
			"mcp_retry_schedule":                  []any{30, 120, 600},
			"mcp_error_codes":                     "sketch",
			"mcp_send_terminated":                 true,
			"mcp_expiry_sweep_interval":           "30s",
			"mcp_max_inflight_per_host":           int64(8),
			"max_mcp_subscriptions_per_tenant":    int64(100),
			"max_mcp_subscriptions_per_principal": int64(20),
			"portal_show_mcp_destinations":        true,
		}
		for key, value := range want {
			require.Contains(t, got, key)
			assert.Equal(t, value, got[key], key)
		}
	})

	t.Run("configured", func(t *testing.T) {
		c := validConfig()
		c.TopicsAllowBreakingChanges = true
		c.MCP.ServerURL = "https://mcp.example.com/mcp?key=hunter2"
		c.MCP.CallbackAllowlist = config.StringList{"localhost", "10.0.0.0/8"}
		c.MCP.ProxyURL = "http://user:hunter2@proxy.internal:3128/?token=hunter2#frag"
		c.MCP.TTLMin = config.Duration(90 * time.Second)
		c.MCP.RetrySchedule = []int{1, 2}
		c.Portal.ShowMCPDestinations = false

		got := summaryValues(t, c)
		assert.Equal(t, true, got["topics_allow_breaking_changes"])
		assert.Equal(t, "https://mcp.example.com/mcp?***", got["mcp_server_url"])
		assert.Equal(t, []any{"localhost", "10.0.0.0/8"}, got["mcp_callback_allowlist"])
		assert.Equal(t, "http://***@proxy.internal:3128/?***", got["mcp_proxy_url"])
		assert.Equal(t, "1m30s", got["mcp_ttl_min"])
		assert.Equal(t, []any{1, 2}, got["mcp_retry_schedule"])
		assert.Equal(t, false, got["portal_show_mcp_destinations"])
		assert.Equal(t, int64(1000), got["retry_poll_backoff_ms"], "the poll backoff follows MCP_RETRY_SCHEDULE")
	})

	// `outpost config list` logs unvalidated config: values that can't be
	// parsed are never printed, since their credentials can't be located.
	t.Run("unparsable proxy URLs are hidden", func(t *testing.T) {
		for _, proxy := range []string{
			"user:hunter2@proxy:3128",
			"http://user:hunter2@proxy:3128 http://user:hunter2@proxy2:3128",
			"//user:hunter2@proxy:3128",
			"http://user:hunter2@",
		} {
			c := validConfig()
			c.MCP.ProxyURL = proxy
			got := summaryValues(t, c)
			assert.Equal(t, "<invalid URL>", got["mcp_proxy_url"], proxy)
		}
	})

	t.Run("summary never contains a secret", func(t *testing.T) {
		c := validConfig()
		c.MCP.ServerURL = "https://u:hunter2@mcp.example.com/?a=hunter2"
		c.MCP.ProxyURL = "socks5://u:hunter2@proxy:1080?x=hunter2"
		for key, value := range summaryValues(t, c) {
			if s, ok := value.(string); ok {
				assert.NotContains(t, s, "hunter2", key)
			}
		}
	})
}
