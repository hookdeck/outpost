package config

import (
	"fmt"
	"math"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
)

const maxDurationSeconds = math.MaxInt64 / int64(time.Second)

type ExperimentalConfig struct {
	MCP MCPConfig `yaml:"mcp"`
}

type MCPConfig struct {
	Enabled                bool            `yaml:"enabled" env:"EXPERIMENTAL_MCP_ENABLED" desc:"If true, registers the experimental 'mcp' destination type (MCP Events webhook subscriptions). Admin API key only. While it is off, stored mcp destinations receive no events and lapse at their expiry." required:"N" default:"false"`
	AllowInsecureCallbacks bool            `yaml:"allow_insecure_callbacks" env:"EXPERIMENTAL_MCP_ALLOW_INSECURE_CALLBACKS" desc:"The MCP Events spec requires https callback URLs. If true, http is also accepted for addresses that are all in DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST: an exception outside the spec, for development only." required:"N" default:"false"`
	RetryDelays            []time.Duration `yaml:"-"`
	VerificationTTLSeconds int             `yaml:"verification_ttl_seconds" env:"EXPERIMENTAL_MCP_VERIFICATION_TTL_SECONDS" desc:"How long, in seconds, a passed callback verification challenge is cached per tenant, principal and callback URL." required:"N" default:"86400"`
}

func (c *MCPConfig) initDefaults() {
	*c = MCPConfig{
		VerificationTTLSeconds: 86400,
	}
}

func invalidMCP(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidMCPConfig, fmt.Sprintf(format, args...))
}

func (c *Config) validateMCP() error {
	m := &c.Experimental.MCP
	if !m.Enabled {
		return nil
	}
	if m.VerificationTTLSeconds < 1 || int64(m.VerificationTTLSeconds) > maxDurationSeconds {
		return invalidMCP("EXPERIMENTAL_MCP_VERIFICATION_TTL_SECONDS (experimental.mcp.verification_ttl_seconds) must be between 1 and %d, got %d", maxDurationSeconds, m.VerificationTTLSeconds)
	}
	return nil
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
	return warnings
}
