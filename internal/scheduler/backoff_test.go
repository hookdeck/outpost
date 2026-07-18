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
