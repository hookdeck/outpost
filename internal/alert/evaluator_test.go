package alert_test

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/stretchr/testify/assert"
)

func TestAlertEvaluator_GetAlertLevel(t *testing.T) {
	tests := []struct {
		name          string
		config        alert.AlertConfig
		failures      int64
		expectedLevel int
		expectedHit   bool
	}{
		{
			name: "no failures",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      0,
			expectedLevel: 0,
			expectedHit:   false,
		},
		{
			name: "empty thresholds",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
			},
			failures:      5,
			expectedLevel: 0,
			expectedHit:   false,
		},
		{
			name: "just hit 50% threshold (5 failures)",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      5,
			expectedLevel: 50,
			expectedHit:   true,
		},
		{
			name: "between 50% and 66% threshold (6 failures = 60%)",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      6,
			expectedLevel: 0,
			expectedHit:   false,
		},
		{
			name: "just hit 66% threshold (7 failures = 70%)",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      7,
			expectedLevel: 66,
			expectedHit:   true,
		},
		{
			name: "between 66% and 90% threshold (8 failures = 80%)",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      8,
			expectedLevel: 0,
			expectedHit:   false,
		},
		{
			name: "just hit 90% threshold (9 failures)",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      9,
			expectedLevel: 90,
			expectedHit:   true,
		},
		{
			name: "just hit 100% threshold (10 failures)",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      10,
			expectedLevel: 100,
			expectedHit:   true,
		},
		{
			name: "over 100% threshold (11 failures)",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      11,
			expectedLevel: 0,
			expectedHit:   false,
		},
		{
			name: "unsorted thresholds",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				AlertThresholds:         []int{90, 50, 100, 66},
			},
			failures:      7,
			expectedLevel: 66,
			expectedHit:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evaluator := alert.NewAlertEvaluator(tt.config)
			level, hit := evaluator.GetAlertLevel(tt.failures)
			assert.Equal(t, tt.expectedLevel, level)
			assert.Equal(t, tt.expectedHit, hit)
		})
	}
}

func TestAlertEvaluator_ShouldAlert(t *testing.T) {
	tests := []struct {
		name          string
		config        alert.AlertConfig
		failures      int64
		lastAlertTime time.Time
		expected      bool
	}{
		{
			name: "empty thresholds",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				DebouncingIntervalMS:    1000,
			},
			failures:      5,
			lastAlertTime: time.Time{},
			expected:      false,
		},
		{
			name: "first alert at threshold",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				DebouncingIntervalMS:    1000,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      5,
			lastAlertTime: time.Time{},
			expected:      true,
		},
		{
			name: "within debouncing interval",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				DebouncingIntervalMS:    1000,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      7,
			lastAlertTime: time.Now().Add(-500 * time.Millisecond),
			expected:      false,
		},
		{
			name: "after debouncing interval at new threshold",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				DebouncingIntervalMS:    1000,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      7,
			lastAlertTime: time.Now().Add(-2000 * time.Millisecond),
			expected:      true,
		},
		{
			name: "after debouncing interval but not at new threshold",
			config: alert.AlertConfig{
				AutoDisableFailureCount: 10,
				DebouncingIntervalMS:    1000,
				AlertThresholds:         []int{50, 66, 90, 100},
			},
			failures:      8,
			lastAlertTime: time.Now().Add(-2000 * time.Millisecond),
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evaluator := alert.NewAlertEvaluator(tt.config)
			result := evaluator.ShouldAlert(tt.failures, tt.lastAlertTime)
			assert.Equal(t, tt.expected, result)
		})
	}
}
