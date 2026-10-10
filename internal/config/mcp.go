package config

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/topicschema"
)

// MCPConfig configures MCP Events subscriptions and the mcp destination type.
// Its env names are absolute (MCP_*); the two subscription limits live on
// Config because their env names don't start with MCP_. Every service reads
// it: the API service handles subscriptions, the delivery service delivers,
// and the log service uses the retry schedule and TTLs.
type MCPConfig struct {
	ServerURL                string     `yaml:"server_url" env:"MCP_SERVER_URL" desc:"Public URL of your MCP server, as an absolute http(s) URL. Filled into the mcp destination type's instructions, which the tenant portal shows in place of a create form." required:"N"`
	CallbackAllowlist        StringList `yaml:"callback_allowlist" env:"MCP_CALLBACK_ALLOWLIST" desc:"Comma-separated IP addresses and CIDR ranges exempt from the callback address check, for local development and tunnels (in YAML, also a list). 'localhost' stands for 127.0.0.0/8 and ::1/128. Host names, wildcards and /0 ranges are rejected; private or loopback ranges log a warning at startup." required:"N"`
	AllowInsecureCallbacks   bool       `yaml:"allow_insecure_callbacks" env:"MCP_ALLOW_INSECURE_CALLBACKS" desc:"If true, allows http callback URLs whose addresses are all in MCP_CALLBACK_ALLOWLIST. Without it, http is only allowed for loopback addresses in the allowlist. Development only." required:"N" default:"false"`
	ProxyURL                 string     `yaml:"proxy_url" env:"MCP_PROXY_URL" desc:"Forward proxy (http, https, socks5 or socks5h URL) for MCP deliveries, verification challenges and terminated envelopes. Outpost still resolves and checks the callback host first, but the proxy connects, so pinning the checked address is its job. DESTINATIONS_PROXY_URL, HTTP_PROXY and HTTPS_PROXY never apply to MCP traffic." required:"N"`
	VerificationTTL          Duration   `yaml:"verification_ttl" env:"MCP_VERIFICATION_TTL" desc:"How long a passed callback verification challenge is cached per tenant, principal and callback URL. A Go duration (e.g. '24h') or a number of seconds." required:"N" default:"24h"`
	VerificationRateLimit    int        `yaml:"verification_rate_limit" env:"MCP_VERIFICATION_RATE_LIMIT" desc:"Verification challenges per tenant and principal per minute; over it, subscribe fails with resource_exhausted. Cached verifications don't count. 0 turns the limit off." required:"N" default:"10"`
	VerificationFailureLimit int        `yaml:"verification_failure_limit" env:"MCP_VERIFICATION_FAILURE_LIMIT" desc:"Failed verification challenges per tenant and callback host per minute; over it, the tenant's challenges to the host fail with resource_exhausted (verification_rate). Ten times it caps unanswered challenges (timeout, connection refused, TLS failure) per callback host per minute across the deployment (callback_host_busy). Successful challenges don't count, and allowlisted hosts are exempt. 0 turns both limits off." required:"N" default:"120"`
	SecretRotationGrace      Duration   `yaml:"secret_rotation_grace" env:"MCP_SECRET_ROTATION_GRACE" desc:"How long Outpost keeps signing with the previous secret after a client rotates it. A Go duration or a number of seconds." required:"N" default:"24h"`
	TTLDefault               Duration   `yaml:"ttl_default" env:"MCP_TTL_DEFAULT" desc:"Subscription lifetime granted when the client suggests none. Between MCP_TTL_MIN and MCP_TTL_MAX. A Go duration or a number of seconds." required:"N" default:"1h"`
	TTLMin                   Duration   `yaml:"ttl_min" env:"MCP_TTL_MIN" desc:"Shortest subscription lifetime granted; shorter suggestions are raised to it. At least 1s. A Go duration or a number of seconds." required:"N" default:"5m"`
	TTLMax                   Duration   `yaml:"ttl_max" env:"MCP_TTL_MAX" desc:"Longest subscription lifetime granted; longer suggestions are lowered to it. A Go duration or a number of seconds." required:"N" default:"24h"`
	AllowNoExpiry            bool       `yaml:"allow_no_expiry" env:"MCP_ALLOW_NO_EXPIRY" desc:"If true, a client asking for no expiry (ttlMs: null) gets a subscription that never expires. Otherwise it gets MCP_TTL_MAX." required:"N" default:"false"`
	RetrySchedule            []int      `yaml:"retry_schedule" env:"MCP_RETRY_SCHEDULE" envSeparator:"," desc:"Comma-separated retry delays in seconds for MCP deliveries (in YAML, a list), each spread by ±20% jitter. Its length is the number of retries. RETRY_SCHEDULE, RETRY_INTERVAL_SECONDS and MAX_RETRY_LIMIT don't apply to MCP." required:"N" default:"30,120,600"`
	ErrorCodes               string     `yaml:"error_codes" env:"MCP_ERROR_CODES" desc:"JSON-RPC error codes in mcp_error and terminated envelopes: 'sketch' (the numbers ChatGPT follows) or 'sep-3415' (SEP-3415's provisional numbers)." required:"N" default:"sketch"`
	SendTerminated           bool       `yaml:"send_terminated" env:"MCP_SEND_TERMINATED" desc:"If true, sends a terminated envelope when Outpost ends a subscription: revocation, topic removal or a forced breaking change." required:"N" default:"true"`
	ExpirySweepInterval      Duration   `yaml:"expiry_sweep_interval" env:"MCP_EXPIRY_SWEEP_INTERVAL" desc:"How often the API service deletes expired subscriptions and ends subscriptions to topics that are no longer MCP-enabled, with 10% jitter. Subscriptions are deleted once expired for twice the interval, 5s to 60s. At least 1s. A Go duration or a number of seconds." required:"N" default:"30s"`
	MaxInFlightPerHost       int        `yaml:"max_inflight_per_host" env:"MCP_MAX_INFLIGHT_PER_HOST" desc:"Delivery attempts, challenges and terminated envelopes in flight per callback host and port, per Outpost process. Over it, an attempt waits up to 5 seconds for a slot, then fails with code throttled and is retried; a challenge waits up to 2 seconds, then subscribe fails with resource_exhausted (callback_host_busy). 0 means no limit." required:"N" default:"8"`
}

// MCP subscription limits (MAX_MCP_SUBSCRIPTIONS_PER_TENANT).
const (
	minMCPSubscriptionsPerTenant = 1
	maxMCPSubscriptionsPerTenant = 10000
)

// mcpMinDeliveryConcurrency is the DELIVERY_MAX_CONCURRENCY below which
// MCPWarnings warns: an MCP attempt can hold a delivery slot for up to 10
// seconds against a URL an agent chose.
const mcpMinDeliveryConcurrency = 8

// mcpProxySchemes are the MCP_PROXY_URL schemes netguard.NewHTTPClient
// accepts.
var mcpProxySchemes = []string{"http", "https", "socks5", "socks5h"}

func (c *MCPConfig) initDefaults() {
	*c = MCPConfig{
		VerificationTTL:          Duration(24 * time.Hour),
		VerificationRateLimit:    10,
		VerificationFailureLimit: 120,
		SecretRotationGrace:      Duration(24 * time.Hour),
		TTLDefault:               Duration(time.Hour),
		TTLMin:                   Duration(5 * time.Minute),
		TTLMax:                   Duration(24 * time.Hour),
		RetrySchedule:            []int{30, 120, 600},
		ErrorCodes:               string(mcpevents.CodeProfileSketch),
		SendTerminated:           true,
		ExpirySweepInterval:      Duration(30 * time.Second),
		MaxInFlightPerHost:       8,
	}
}

// invalidMCP returns an ErrInvalidMCPConfig error with the given detail.
func invalidMCP(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidMCPConfig, fmt.Sprintf(format, args...))
}

// validateMCP checks the MCP settings and the MCP subscription limits. It
// does no I/O.
func (c *Config) validateMCP() error {
	m := &c.MCP

	if err := validateMCPServerURL(m.ServerURL); err != nil {
		return err
	}
	if _, _, err := c.MCPAllowlist(); err != nil {
		return err
	}
	if _, err := c.MCPProxyURL(); err != nil {
		return err
	}

	// TTLs are granted to the millisecond but refreshBefore is reported in
	// whole seconds, so a lifetime under a second would tell clients to
	// refresh before the time they subscribed.
	for _, ttl := range []struct {
		name  string
		value Duration
	}{
		{"MCP_TTL_MIN (mcp.ttl_min)", m.TTLMin},
		{"MCP_TTL_DEFAULT (mcp.ttl_default)", m.TTLDefault},
		{"MCP_TTL_MAX (mcp.ttl_max)", m.TTLMax},
	} {
		if ttl.value < Duration(time.Second) {
			return invalidMCP("%s must be at least 1s, got %s", ttl.name, ttl.value)
		}
	}
	if m.TTLMin > m.TTLDefault || m.TTLDefault > m.TTLMax {
		return invalidMCP("MCP_TTL_MIN (%s), MCP_TTL_DEFAULT (%s) and MCP_TTL_MAX (%s) must be in increasing order or equal",
			m.TTLMin, m.TTLDefault, m.TTLMax)
	}
	if m.VerificationTTL <= 0 {
		return invalidMCP("MCP_VERIFICATION_TTL (mcp.verification_ttl) must be greater than 0, got %s", m.VerificationTTL)
	}
	if m.SecretRotationGrace <= 0 {
		return invalidMCP("MCP_SECRET_ROTATION_GRACE (mcp.secret_rotation_grace) must be greater than 0, got %s", m.SecretRotationGrace)
	}
	if m.ExpirySweepInterval < Duration(time.Second) {
		return invalidMCP("MCP_EXPIRY_SWEEP_INTERVAL (mcp.expiry_sweep_interval) must be at least 1s, got %s", m.ExpirySweepInterval)
	}
	if m.VerificationRateLimit < 0 {
		return invalidMCP("MCP_VERIFICATION_RATE_LIMIT (mcp.verification_rate_limit) must be 0 (no limit) or more, got %d", m.VerificationRateLimit)
	}
	if m.VerificationFailureLimit < 0 {
		return invalidMCP("MCP_VERIFICATION_FAILURE_LIMIT (mcp.verification_failure_limit) must be 0 (no limit) or more, got %d", m.VerificationFailureLimit)
	}
	if m.MaxInFlightPerHost < 0 {
		return invalidMCP("MCP_MAX_INFLIGHT_PER_HOST (mcp.max_inflight_per_host) must be 0 (no limit) or more, got %d", m.MaxInFlightPerHost)
	}
	if len(m.RetrySchedule) == 0 {
		return invalidMCP("MCP_RETRY_SCHEDULE (mcp.retry_schedule) must list at least one delay")
	}
	for i, seconds := range m.RetrySchedule {
		if seconds < 1 {
			return invalidMCP("MCP_RETRY_SCHEDULE (mcp.retry_schedule) entries must be at least 1 second, got %d at index %d", seconds, i)
		}
		if int64(seconds) > maxDurationSeconds {
			return invalidMCP("MCP_RETRY_SCHEDULE (mcp.retry_schedule) entry %d at index %d is too large", seconds, i)
		}
	}
	if _, err := parseMCPCodeProfile(m.ErrorCodes); err != nil {
		return invalidMCP("MCP_ERROR_CODES (mcp.error_codes) must be %q or %q, got %q",
			mcpevents.CodeProfileSketch, mcpevents.CodeProfileSEP3415, m.ErrorCodes)
	}

	if n := c.MaxMCPSubscriptionsPerTenant; n < minMCPSubscriptionsPerTenant || n > maxMCPSubscriptionsPerTenant {
		return invalidMCP("MAX_MCP_SUBSCRIPTIONS_PER_TENANT (max_mcp_subscriptions_per_tenant) must be between %d and %d, got %d",
			minMCPSubscriptionsPerTenant, maxMCPSubscriptionsPerTenant, n)
	}
	if c.MaxMCPSubscriptionsPerPrincipal < 0 {
		return invalidMCP("MAX_MCP_SUBSCRIPTIONS_PER_PRINCIPAL (max_mcp_subscriptions_per_principal) must be 0 (no limit) or more, got %d",
			c.MaxMCPSubscriptionsPerPrincipal)
	}
	return nil
}

// validateMCPServerURL requires an absolute http(s) URL without credentials
// or whitespace: it is shown to tenants in the portal's setup instructions.
func validateMCPServerURL(raw string) error {
	if raw == "" {
		return nil
	}
	const name = "MCP_SERVER_URL (mcp.server_url)"
	if strings.ContainsFunc(raw, isSpaceOrControl) {
		return invalidMCP("%s must not contain whitespace", name)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Opaque != "" {
		return invalidMCP("%s must be an absolute http or https URL", name)
	}
	if u.User != nil {
		return invalidMCP("%s must not contain credentials: it is shown to tenants", name)
	}
	return nil
}

func isSpaceOrControl(r rune) bool {
	return r <= ' ' || r == 0x7f
}

// parseMCPCodeProfile parses MCP_ERROR_CODES, ignoring case and surrounding
// whitespace.
func parseMCPCodeProfile(s string) (mcpevents.CodeProfile, error) {
	return mcpevents.ParseCodeProfile(strings.ToLower(strings.TrimSpace(s)))
}

// mcpRetryJitter spreads each MCP retry delay by ±20%: subscriptions on one
// callback host (ChatGPT's) that failed together, a throttled burst for
// one, don't all retry together.
const mcpRetryJitter = 0.2

// MCPRetryBackoff returns the retry policy for MCP deliveries: a scheduled
// backoff over MCP_RETRY_SCHEDULE with ±20% jitter, and the schedule length
// as the max number of retries. Wire it with
// deliverymq.WithRetryPolicy("mcp", ...).
func (c *Config) MCPRetryBackoff() (backoff.Backoff, int) {
	schedule := make([]time.Duration, len(c.MCP.RetrySchedule))
	for i, seconds := range c.MCP.RetrySchedule {
		schedule[i] = time.Duration(seconds) * time.Second
	}
	return &backoff.JitteredBackoff{Backoff: &backoff.ScheduledBackoff{Schedule: schedule}, Jitter: mcpRetryJitter}, len(schedule)
}

// MCPTTLConfig returns the subscription lifetime settings for
// mcpevents.GrantTTL.
func (c *Config) MCPTTLConfig() mcpevents.TTLConfig {
	return mcpevents.TTLConfig{
		Default:       c.MCP.TTLDefault.Duration(),
		Min:           c.MCP.TTLMin.Duration(),
		Max:           c.MCP.TTLMax.Duration(),
		AllowNoExpiry: c.MCP.AllowNoExpiry,
	}
}

// MCPAllowlist parses MCP_CALLBACK_ALLOWLIST for netguard.Guard. warnings
// name entries that open non-global address space, for startup logging (they
// are also part of MCPWarnings). An empty allowlist is a non-nil empty
// *netguard.Allowlist.
func (c *Config) MCPAllowlist() (*netguard.Allowlist, []string, error) {
	allowlist, warnings, err := netguard.ParseAllowlist(c.MCP.CallbackAllowlist)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: MCP_CALLBACK_ALLOWLIST (mcp.callback_allowlist): %w", ErrInvalidMCPConfig, err)
	}
	return allowlist, warnings, nil
}

// MCPProxyURL parses MCP_PROXY_URL for netguard.ClientConfig.ProxyURL. It
// returns nil when unset. Errors never include the URL, which may carry
// credentials.
func (c *Config) MCPProxyURL() (*url.URL, error) {
	raw := c.MCP.ProxyURL
	if raw == "" {
		return nil, nil
	}
	const name = "MCP_PROXY_URL (mcp.proxy_url)"
	if strings.ContainsFunc(raw, isSpaceOrControl) {
		return nil, invalidMCP("%s must be a single proxy URL without whitespace", name)
	}
	u, err := url.Parse(raw)
	if err != nil {
		// The parse error repeats parts of the URL, which may be credentials.
		return nil, invalidMCP("%s is not a valid URL", name)
	}
	if !slices.Contains(mcpProxySchemes, u.Scheme) {
		return nil, invalidMCP("%s scheme must be one of %s", name, strings.Join(mcpProxySchemes, ", "))
	}
	if u.Hostname() == "" {
		return nil, invalidMCP("%s has no host", name)
	}
	return u, nil
}

// MCPCodeProfile returns the MCP_ERROR_CODES profile; sketch when the value
// is invalid (Validate rejects it).
func (c *Config) MCPCodeProfile() mcpevents.CodeProfile {
	profile, err := parseMCPCodeProfile(c.MCP.ErrorCodes)
	if err != nil {
		return mcpevents.CodeProfileSketch
	}
	return profile
}

// MCPVerificationLimits returns MCP_VERIFICATION_RATE_LIMIT and
// MCP_VERIFICATION_FAILURE_LIMIT for mcpevents.VerifierConfig, where 0 means
// the default and a negative value turns the limit off: a configured 0 (no
// limit) becomes -1.
func (c *Config) MCPVerificationLimits() (rateLimit, failureLimit int) {
	off := func(n int) int {
		if n <= 0 {
			return -1
		}
		return n
	}
	return off(c.MCP.VerificationRateLimit), off(c.MCP.VerificationFailureLimit)
}

// MCPWarnings returns startup warnings about the MCP settings: allowlist
// entries opening non-global address space, insecure callbacks, and, when
// catalog has MCP-enabled topics, a DELIVERY_MAX_CONCURRENCY too low for
// MCP's 10 second attempts. catalog may be nil (services that don't load
// it).
func (c *Config) MCPWarnings(catalog *topicschema.Catalog) []string {
	var warnings []string
	if catalog.MCPEnabled() && c.DeliveryMaxConcurrency < mcpMinDeliveryConcurrency {
		warnings = append(warnings, fmt.Sprintf(
			"DELIVERY_MAX_CONCURRENCY is %d with MCP-enabled topics: each MCP delivery attempt can hold a delivery slot for up to 10 seconds against a URL an agent chose, so slow callbacks can delay every delivery. Set it to at least %d.",
			c.DeliveryMaxConcurrency, mcpMinDeliveryConcurrency))
	}
	allowlist, allowlistWarnings, err := c.MCPAllowlist()
	if err == nil {
		for _, w := range allowlistWarnings {
			warnings = append(warnings, "MCP_CALLBACK_ALLOWLIST: "+w)
		}
	}
	if c.MCP.AllowInsecureCallbacks {
		if err == nil && len(allowlist.Prefixes()) == 0 {
			warnings = append(warnings, "MCP_ALLOW_INSECURE_CALLBACKS is true but MCP_CALLBACK_ALLOWLIST is empty, so it has no effect.")
		} else {
			warnings = append(warnings, "MCP_ALLOW_INSECURE_CALLBACKS is true: plain http callback URLs are accepted for allowlisted addresses, sending event payloads unencrypted. Use it for development only.")
		}
	}
	return warnings
}
