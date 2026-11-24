package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalculateExecBackoff(t *testing.T) {
	tests := []struct {
		name         string
		base         time.Duration
		receiveCount uint64
		maxBackoff   time.Duration
		want         time.Duration
	}{
		{
			name:         "first receive uses base",
			base:         30 * time.Second,
			receiveCount: 1,
			maxBackoff:   15 * time.Minute,
			want:         30 * time.Second,
		},
		{
			name:         "backoff doubles per receive",
			base:         30 * time.Second,
			receiveCount: 5,
			maxBackoff:   15 * time.Minute,
			want:         8 * time.Minute,
		},
		{
			name:         "backoff is capped",
			base:         30 * time.Second,
			receiveCount: 6,
			maxBackoff:   15 * time.Minute,
			want:         15 * time.Minute,
		},
		{
			name:         "base above cap is capped on first receive",
			base:         30 * time.Minute,
			receiveCount: 1,
			maxBackoff:   15 * time.Minute,
			want:         15 * time.Minute,
		},
		{
			name:         "large receive count does not overflow",
			base:         30 * time.Second,
			receiveCount: ^uint64(0),
			maxBackoff:   15 * time.Minute,
			want:         15 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, calculateExecBackoff(tt.base, tt.receiveCount, tt.maxBackoff))
		})
	}
}

func TestJitterBackoff(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		max  time.Duration
		r    float64
		want time.Duration
	}{
		{"low end is 0.75x", 100 * time.Second, 15 * time.Minute, 0, 75 * time.Second},
		{"middle is 1x", 100 * time.Second, 15 * time.Minute, 0.5, 100 * time.Second},
		{"high end stays below 1.25x", 100 * time.Second, 15 * time.Minute, 0.999, 125 * time.Second},
		{"clamped to max", 10 * time.Minute, 11 * time.Minute, 0.999, 11 * time.Minute},
		{"at max spreads over the lower half", 15 * time.Minute, 15 * time.Minute, 0, 675 * time.Second},
		{"at max never exceeds max", 15 * time.Minute, 15 * time.Minute, 0.999, 15 * time.Minute},
		{"floor is 1s", 1 * time.Second, 15 * time.Minute, 0, 1 * time.Second},
		{"zero stays zero", 0, 15 * time.Minute, 0.5, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, jitterBackoff(tt.d, tt.max, tt.r))
		})
	}

	t.Run("always within bounds", func(t *testing.T) {
		for r := 0.0; r < 1; r += 0.01 {
			got := jitterBackoff(240*time.Second, 15*time.Minute, r)
			require.GreaterOrEqual(t, got, 180*time.Second)
			require.LessOrEqual(t, got, 300*time.Second)
		}
	})
}
