package mcpevents

import (
	"math"
	"time"
)

// DefaultSubscriptionTTL is the lifetime granted when a request has no ttlMs.
const DefaultSubscriptionTTL = 24 * time.Hour

// maxExpiry is the latest expiry granted: the last instant RFC 3339 can write.
var maxExpiry = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

// GrantTTL returns the expiry granted for a requested ttlMs, truncated to
// milliseconds, or nil for no expiry:
//   - absent: now + DefaultSubscriptionTTL;
//   - a number: exactly that many whole milliseconds from now; zero or less
//     expires now;
//   - null: no expiry.
//
// A number beyond the year 9999 is granted until then.
func GrantTTL(req TTLRequest, now time.Time) *time.Time {
	var expiresAt time.Time
	switch req.Kind {
	case TTLNull:
		return nil
	case TTLFinite:
		ms := math.Max(math.Floor(req.Ms), 0)
		// Duration saturates after 292 years, so add in milliseconds since the epoch.
		if limit := float64(maxExpiry.Unix()-now.Unix()) * 1000; ms > limit {
			expiresAt = maxExpiry
			break
		}
		expiresAt = time.UnixMilli(now.UnixMilli() + int64(ms))
	default:
		expiresAt = now.Add(DefaultSubscriptionTTL)
	}
	expiresAt = expiresAt.UTC().Truncate(time.Millisecond)
	return &expiresAt
}

// FormatRefreshBefore renders a grant as RFC 3339 UTC truncated to seconds,
// never later than the real expiry, or nil for no expiry.
func FormatRefreshBefore(expiresAt *time.Time) *string {
	if expiresAt == nil {
		return nil
	}
	s := expiresAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	return &s
}
