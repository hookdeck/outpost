package mcpevents

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrantTTL(t *testing.T) {
	t.Parallel()
	cfg := TTLConfig{Default: time.Hour, Min: 5 * time.Minute, Max: 24 * time.Hour}
	noExpiry := cfg
	noExpiry.AllowNoExpiry = true
	now := time.Date(2026, 10, 9, 17, 0, 0, 123456789, time.FixedZone("x", 3600))
	nowMs := now.UTC().Truncate(time.Millisecond)

	tests := []struct {
		name string
		req  TTLRequest
		cfg  TTLConfig
		want time.Duration // from now (ms-truncated); -1 for no expiry
	}{
		{"absent → default", TTLRequest{Kind: TTLAbsent}, cfg, time.Hour},
		{"finite within range", TTLRequest{Kind: TTLFinite, Ms: 2 * 3600000}, cfg, 2 * time.Hour},
		{"finite floored", TTLRequest{Kind: TTLFinite, Ms: 600000.9}, cfg, 10 * time.Minute},
		{"below min clamps up", TTLRequest{Kind: TTLFinite, Ms: 1000}, cfg, 5 * time.Minute},
		{"zero clamps up", TTLRequest{Kind: TTLFinite, Ms: 0}, cfg, 5 * time.Minute},
		{"negative clamps to min", TTLRequest{Kind: TTLFinite, Ms: -3600000}, cfg, 5 * time.Minute},
		{"huge negative clamps to min", TTLRequest{Kind: TTLFinite, Ms: -math.MaxFloat64}, cfg, 5 * time.Minute},
		{"above max clamps down", TTLRequest{Kind: TTLFinite, Ms: 7 * 24 * 3600000}, cfg, 24 * time.Hour},
		{"huge clamps to max", TTLRequest{Kind: TTLFinite, Ms: math.MaxFloat64}, cfg, 24 * time.Hour},
		{"exactly min", TTLRequest{Kind: TTLFinite, Ms: 300000}, cfg, 5 * time.Minute},
		{"exactly max", TTLRequest{Kind: TTLFinite, Ms: 86400000}, cfg, 24 * time.Hour},
		{"null without no-expiry → max", TTLRequest{Kind: TTLNull}, cfg, 24 * time.Hour},
		{"null with no-expiry → none", TTLRequest{Kind: TTLNull}, noExpiry, -1},
		{"finite with no-expiry still finite", TTLRequest{Kind: TTLFinite, Ms: 3600000}, noExpiry, time.Hour},
		{"absent with no-expiry → default", TTLRequest{Kind: TTLAbsent}, noExpiry, time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GrantTTL(tt.req, tt.cfg, now)
			if tt.want < 0 {
				assert.Nil(t, got)
				assert.Nil(t, FormatRefreshBefore(got))
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, nowMs.Add(tt.want), *got)
			assert.Equal(t, time.UTC, got.Location())
			assert.Zero(t, got.Nanosecond()%int(time.Millisecond), "truncated to ms")
		})
	}
}

func TestFormatRefreshBefore(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 9, 18, 0, 0, 999000000, time.FixedZone("x", -7200))
	got := FormatRefreshBefore(&at)
	require.NotNil(t, got)
	assert.Equal(t, "2026-10-09T20:00:00Z", *got, "UTC, truncated (never later than the expiry)")

	// Explicit null when serialized without omitempty.
	body, err := json.Marshal(struct {
		RefreshBefore *string `json:"refreshBefore"`
	}{FormatRefreshBefore(nil)})
	require.NoError(t, err)
	assert.JSONEq(t, `{"refreshBefore":null}`, string(body))
}
