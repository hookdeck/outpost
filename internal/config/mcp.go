package config

import (
	"fmt"
	"math"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
	"github.com/hookdeck/outpost/internal/mcpevents"
)

const maxDurationSeconds = math.MaxInt64 / int64(time.Second)

type ExperimentalConfig struct {
	MCP MCPConfig `yaml:"mcp"`
}

type MCPConfig struct {
	Enabled                       bool            `yaml:"enabled" env:"EXPERIMENTAL_MCP_ENABLED" desc:"If true, registers the experimental 'mcp' destination type (MCP Events webhook subscriptions). Admin API key only. While it is off, stored mcp destinations receive no events and lapse at their expiry." required:"N" default:"false"`
	AllowInsecureCallbacks        bool            `yaml:"allow_insecure_callbacks" env:"EXPERIMENTAL_MCP_ALLOW_INSECURE_CALLBACKS" desc:"The MCP Events spec requires https callback URLs. If true, http is also accepted for addresses that are all in DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST: an exception outside the spec, for development only." required:"N" default:"false"`
	RetryDelays                   []time.Duration `yaml:"-"`
	VerificationTTLSeconds        int             `yaml:"verification_ttl_seconds" env:"EXPERIMENTAL_MCP_VERIFICATION_TTL_SECONDS" desc:"How long, in seconds, a passed callback verification challenge is cached per tenant, principal and callback URL." required:"N" default:"86400"`
	SubscriptionTTLDefaultSeconds int             `yaml:"subscription_ttl_default_seconds" env:"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_DEFAULT_SECONDS" desc:"Lifetime, in seconds, of a subscription made through the MCP subscribe endpoint when the request has no ttlMs. Must be between the min and max." required:"N" default:"3600"`
	SubscriptionTTLMinSeconds     int             `yaml:"subscription_ttl_min_seconds" env:"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MIN_SECONDS" desc:"Shortest lifetime, in seconds, granted through the MCP subscribe endpoint; shorter requests are raised to it." required:"N" default:"300"`
	SubscriptionTTLMaxSeconds     int             `yaml:"subscription_ttl_max_seconds" env:"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MAX_SECONDS" desc:"Longest lifetime, in seconds, granted through the MCP subscribe endpoint; longer requests and a null ttlMs get this." required:"N" default:"86400"`
	EventsSchema                  MCPEventsSchema `yaml:"events_schema" env:"EXPERIMENTAL_MCP_EVENTS_SCHEMA" desc:"Event definitions for MCP subscriptions in the MCP events/list format: {\"events\": [...]}, each with a name, a payloadSchema and an optional inputSchema (JSON Schema 2020-12 objects; without it, every scalar top-level payload property is an optional argument). In YAML a mapping (inline definitions) or a path to a JSON or YAML file (a string is always a path); in the environment inline JSON (a value starting with {) or a file path. Every name must be in TOPICS when TOPICS is set. Invalid definitions don't stop Outpost: it logs the error and MCP Events answers 503." required:"N"`
}

func (c *MCPConfig) initDefaults() {
	*c = MCPConfig{
		VerificationTTLSeconds: 86400,

		SubscriptionTTLDefaultSeconds: 3600,
		SubscriptionTTLMinSeconds:     300,
		SubscriptionTTLMaxSeconds:     86400,
	}
}

func invalidMCP(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidMCPConfig, fmt.Sprintf(format, args...))
}

func (c *Config) validateMCP() error {
	m := &c.Experimental.MCP
	if !m.Enabled {
		c.mcpCatalog = nil
		c.mcpEventsErr = nil
		return nil
	}
	if m.VerificationTTLSeconds < 1 || int64(m.VerificationTTLSeconds) > maxDurationSeconds {
		return invalidMCP("EXPERIMENTAL_MCP_VERIFICATION_TTL_SECONDS (experimental.mcp.verification_ttl_seconds) must be between 1 and %d, got %d", maxDurationSeconds, m.VerificationTTLSeconds)
	}
	if err := m.validateSubscriptionTTL(); err != nil {
		return err
	}
	c.validateMCPEvents()
	return nil
}

func (m *MCPConfig) validateSubscriptionTTL() error {
	for _, v := range []struct {
		env, key string
		seconds  int
	}{
		{"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_DEFAULT_SECONDS", "experimental.mcp.subscription_ttl_default_seconds", m.SubscriptionTTLDefaultSeconds},
		{"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MIN_SECONDS", "experimental.mcp.subscription_ttl_min_seconds", m.SubscriptionTTLMinSeconds},
		{"EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MAX_SECONDS", "experimental.mcp.subscription_ttl_max_seconds", m.SubscriptionTTLMaxSeconds},
	} {
		if v.seconds < 1 || int64(v.seconds) > maxDurationSeconds {
			return invalidMCP("%s (%s) must be between 1 and %d, got %d", v.env, v.key, maxDurationSeconds, v.seconds)
		}
	}
	if m.SubscriptionTTLMinSeconds > m.SubscriptionTTLDefaultSeconds || m.SubscriptionTTLDefaultSeconds > m.SubscriptionTTLMaxSeconds {
		return invalidMCP("EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_DEFAULT_SECONDS (experimental.mcp.subscription_ttl_default_seconds) must be between EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MIN_SECONDS (%d) and EXPERIMENTAL_MCP_SUBSCRIPTION_TTL_MAX_SECONDS (%d), got %d",
			m.SubscriptionTTLMinSeconds, m.SubscriptionTTLMaxSeconds, m.SubscriptionTTLDefaultSeconds)
	}
	return nil
}

// MCPSubscriptionTTL returns the lifetime rules of the MCP subscribe endpoint,
// or nil when MCP is off.
func (c *Config) MCPSubscriptionTTL() *mcpevents.TTLConfig {
	if !c.Experimental.MCP.Enabled {
		return nil
	}
	return &mcpevents.TTLConfig{
		Default: time.Duration(c.Experimental.MCP.SubscriptionTTLDefaultSeconds) * time.Second,
		Min:     time.Duration(c.Experimental.MCP.SubscriptionTTLMinSeconds) * time.Second,
		Max:     time.Duration(c.Experimental.MCP.SubscriptionTTLMaxSeconds) * time.Second,
	}
}

// mcpRetrySchedule is the fixed delay before each retry of an mcp delivery:
// five attempts within 14 minutes of the first.
var mcpRetrySchedule = []time.Duration{1 * time.Minute, 3 * time.Minute, 5 * time.Minute, 5 * time.Minute}

func (c *Config) mcpRetryDelays() []time.Duration {
	if len(c.Experimental.MCP.RetryDelays) > 0 {
		return c.Experimental.MCP.RetryDelays
	}
	return mcpRetrySchedule
}

func (c *Config) MCPRetryBackoff() (backoff.Backoff, int) {
	schedule := c.mcpRetryDelays()
	return c.floorBackoff(&backoff.ScheduledBackoff{Schedule: schedule}), len(schedule)
}

func (c *Config) MCPWarnings() []string {
	if !c.Experimental.MCP.Enabled {
		return nil
	}
	var warnings []string
	if c.Experimental.MCP.AllowInsecureCallbacks {
		if len(c.Destinations.PrivateAddressAllowlist) == 0 {
			warnings = append(warnings, "EXPERIMENTAL_MCP_ALLOW_INSECURE_CALLBACKS is true but DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST is empty, so it has no effect.")
		} else {
			warnings = append(warnings, "EXPERIMENTAL_MCP_ALLOW_INSECURE_CALLBACKS is true: plain http callback URLs are accepted for allowlisted addresses, sending event payloads unencrypted. Use it for development only.")
		}
	}
	if c.mcpCatalog == nil && c.mcpEventsErr == nil {
		warnings = append(warnings, "EXPERIMENTAL_MCP_ENABLED is true but EXPERIMENTAL_MCP_EVENTS_SCHEMA (experimental.mcp.events_schema) defines no events: every mcp subscription is refused.")
	}
	return warnings
}
