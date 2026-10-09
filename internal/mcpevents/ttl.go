package mcpevents

import (
	"math"
	"time"
)

// TTLConfig holds the subscription lifetime settings (MCP_TTL_DEFAULT, _MIN,
// _MAX, MCP_ALLOW_NO_EXPIRY). Config validation keeps Min <= Default <= Max.
type TTLConfig struct {
	Default       time.Duration
	Min           time.Duration
	Max           time.Duration
	AllowNoExpiry bool
}

// GrantTTL returns the granted expiry for a requested ttlMs, truncated to
// milliseconds, or nil for no expiry:
//   - absent: now + Default;
//   - finite: floored to whole milliseconds and clamped to [Min, Max], so a
//     negative suggestion gets Min (there is no rejection path for TTLs);
//   - null: no expiry when AllowNoExpiry, else now + Max.
func GrantTTL(req TTLRequest, cfg TTLConfig, now time.Time) *time.Time {
	var ttl time.Duration
	switch req.Kind {
	case TTLNull:
		if cfg.AllowNoExpiry {
			return nil
		}
		ttl = cfg.Max
	case TTLFinite:
		ms := math.Floor(req.Ms)
		ms = math.Max(ms, float64(cfg.Min.Milliseconds()))
		ms = math.Min(ms, float64(cfg.Max.Milliseconds()))
		ttl = time.Duration(ms) * time.Millisecond
	default:
		ttl = cfg.Default
	}
	expiresAt := now.Add(ttl).UTC().Truncate(time.Millisecond)
	return &expiresAt
}

// FormatRefreshBefore renders a grant as the subscribe result's refreshBefore:
// RFC 3339 UTC truncated to seconds (never later than the real expiry), or nil
// (an explicit JSON null; don't use omitempty) for no expiry.
func FormatRefreshBefore(expiresAt *time.Time) *string {
	if expiresAt == nil {
		return nil
	}
	s := expiresAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	return &s
}
