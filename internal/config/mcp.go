package config

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/backoff"
)

const maxDurationSeconds = math.MaxInt64 / int64(time.Second)

type ExperimentalConfig struct {
	MCP MCPConfig `yaml:"mcp"`
}

type MCPConfig struct {
	Enabled                bool  `yaml:"enabled" env:"EXPERIMENTAL_MCP_ENABLED" desc:"If true, registers the experimental 'mcp' destination type (MCP Events webhook subscriptions). Admin API key only. Delete mcp destinations before turning it off; while it is off, they stay listed and each event fails once without retries." required:"N" default:"false"`
	AllowInsecureCallbacks bool  `yaml:"allow_insecure_callbacks" env:"EXPERIMENTAL_MCP_ALLOW_INSECURE_CALLBACKS" desc:"The MCP Events spec requires https callback URLs. If true, http is also accepted for addresses that are all in DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST: an exception outside the spec, for development only." required:"N" default:"false"`
	RetrySchedule          []int `yaml:"retry_schedule" env:"EXPERIMENTAL_MCP_RETRY_SCHEDULE" envSeparator:"," desc:"Comma-separated retry delays in seconds for mcp deliveries (in YAML, a list). Its length is the number of retries. RETRY_SCHEDULE, RETRY_INTERVAL_SECONDS and MAX_RETRY_LIMIT don't apply to mcp. Delays shorter than LOG_BATCH_THRESHOLD_SECONDS are raised to it." required:"N" default:"30,120,600"`
	VerificationTTLSeconds int   `yaml:"verification_ttl_seconds" env:"EXPERIMENTAL_MCP_VERIFICATION_TTL_SECONDS" desc:"How long, in seconds, a passed callback verification challenge is cached per tenant, principal and callback URL." required:"N" default:"86400"`
}

func (c *MCPConfig) initDefaults() {
	*c = MCPConfig{
		RetrySchedule:          []int{30, 120, 600},
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
	if len(m.RetrySchedule) == 0 {
		return invalidMCP("EXPERIMENTAL_MCP_RETRY_SCHEDULE (experimental.mcp.retry_schedule) must list at least one delay")
	}
	for i, seconds := range m.RetrySchedule {
		if seconds < 1 {
			return invalidMCP("EXPERIMENTAL_MCP_RETRY_SCHEDULE (experimental.mcp.retry_schedule) entries must be at least 1 second, got %d at index %d", seconds, i)
		}
		if int64(seconds) > maxDurationSeconds {
			return invalidMCP("EXPERIMENTAL_MCP_RETRY_SCHEDULE (experimental.mcp.retry_schedule) entry %d at index %d is too large", seconds, i)
		}
	}
	return nil
}

func (c *Config) MCPRetryBackoff() (backoff.Backoff, int) {
	schedule := make([]time.Duration, len(c.Experimental.MCP.RetrySchedule))
	for i, seconds := range c.Experimental.MCP.RetrySchedule {
		schedule[i] = time.Duration(seconds) * time.Second
	}
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
	if floor := time.Duration(c.LogBatchThresholdSeconds) * time.Second; floor > 0 {
		var raised []string
		for _, seconds := range c.Experimental.MCP.RetrySchedule {
			if d := time.Duration(seconds) * time.Second; d < floor {
				raised = append(raised, d.String())
			}
		}
		if len(raised) > 0 {
			warnings = append(warnings, fmt.Sprintf("EXPERIMENTAL_MCP_RETRY_SCHEDULE: delays below LOG_BATCH_THRESHOLD_SECONDS (%s) are raised to it: %s", floor, strings.Join(raised, ", ")))
		}
	}
	return warnings
}
