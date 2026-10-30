package mcpevents

import (
	"math"
	"time"
)

// TTLConfig holds the subscription lifetime rules; Min <= Default <= Max.
type TTLConfig struct {
	Default time.Duration
	Min     time.Duration
	Max     time.Duration
}

// GrantTTL returns the expiry granted for a requested ttlMs, truncated to
// milliseconds:
//   - absent: now + Default;
//   - a number: floored to whole milliseconds and clamped to [Min, Max];
//   - null: now + Max (a request for no expiry still gets a finite grant).
func GrantTTL(req TTLRequest, cfg TTLConfig, now time.Time) time.Time {
	var ttl time.Duration
	switch req.Kind {
	case TTLNull:
		ttl = cfg.Max
	case TTLFinite:
		ms := math.Floor(req.Ms)
		ms = math.Max(ms, float64(cfg.Min.Milliseconds()))
		ms = math.Min(ms, float64(cfg.Max.Milliseconds()))
		ttl = time.Duration(ms) * time.Millisecond
	default:
		ttl = cfg.Default
	}
	return now.Add(ttl).UTC().Truncate(time.Millisecond)
}

// FormatRefreshBefore renders a grant as RFC 3339 UTC truncated to seconds,
// never later than the real expiry.
func FormatRefreshBefore(expiresAt time.Time) string {
	return expiresAt.UTC().Truncate(time.Second).Format(time.RFC3339)
}
