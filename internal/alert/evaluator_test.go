package alert_test

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/stretchr/testify/assert"
)

type alertResult struct {
	level       int
	shouldAlert bool
}

func TestAlertEvaluator_GetAlertLevel(t *testing.T) {
	t.Parallel()

	t.Run("with 100 threshold", func(t *testing.T) {
		t.Parallel()
		evaluator := alert.NewAlertEvaluator(
			[]int{50, 66, 90, 100},
			10, // autoDisableFailureCount
			0,  // debouncingIntervalMS
		)

		tests := []struct {
			failures int64
			want     int
		}{
			{failures: 4, want: 0},    // Below first threshold
			{failures: 5, want: 50},   // At first threshold (50%)
			{failures: 6, want: 50},   // Past first threshold
			{failures: 7, want: 66},   // At second threshold (66%)
			{failures: 8, want: 66},   // Past second threshold
			{failures: 9, want: 90},   // At third threshold (90%)
			{failures: 10, want: 100}, // At final threshold (100%)
			{failures: 11, want: 100}, // Past final threshold
		}

		for _, tt := range tests {
			level := evaluator.GetAlertLevel(tt.failures)
			assert.Equal(t, tt.want, level)
		}
	})

	t.Run("auto-includes 100 threshold", func(t *testing.T) {
		t.Parallel()
		evaluator := alert.NewAlertEvaluator(
			[]int{50, 66, 90}, // No 100% threshold
			10,                // autoDisableFailureCount
			0,                 // debouncingIntervalMS
		)

		// At auto-disable threshold should give 100% alert
		level := evaluator.GetAlertLevel(10)
		assert.Equal(t, 100, level, "should auto-include 100% threshold")
	})

	t.Run("prunes invalid thresholds", func(t *testing.T) {
		t.Parallel()
		evaluator := alert.NewAlertEvaluator(
			[]int{-5, 0, 101, 150}, // Only invalid thresholds
			100,                    // Makes percentages match failure counts
			0,                      // debouncingIntervalMS
		)

		// Test that invalid thresholds are pruned and 100 is added
		tests := []struct {
			failures int64
			want     int
		}{
			{failures: 0, want: 0},     // Zero failures
			{failures: 100, want: 100}, // At auto-disable threshold
			{failures: 101, want: 100}, // Above auto-disable threshold
			{failures: 150, want: 100}, // Well above auto-disable threshold
		}

		for _, tt := range tests {
			level := evaluator.GetAlertLevel(tt.failures)
			assert.Equal(t, tt.want, level, "failures=%d", tt.failures)
		}
	})
}

func TestAlertEvaluator_ShouldAlert_ZeroDebounce(t *testing.T) {
	t.Parallel()

	t.Run("with valid thresholds", func(t *testing.T) {
		t.Parallel()
		evaluator := alert.NewAlertEvaluator(
			[]int{50, 66, 90, 100},
			10, // autoDisableFailureCount
			0,  // No debouncing
		)

		tests := []struct {
			name          string
			failures      int64
			lastAlertTime time.Time
			lastLevel     int
			want          alertResult
		}{
			{
				name:      "no failures",
				failures:  0,
				lastLevel: 0,
				want:      alertResult{level: 0, shouldAlert: false},
			},
			{
				name:      "below first threshold",
				failures:  4,
				lastLevel: 0,
				want:      alertResult{level: 0, shouldAlert: false},
			},
			{
				name:      "at first threshold",
				failures:  5,
				lastLevel: 0,
				want:      alertResult{level: 50, shouldAlert: true},
			},
			{
				name:          "at same threshold again",
				failures:      6,
				lastAlertTime: time.Now(),
				lastLevel:     50,
				want:          alertResult{level: 50, shouldAlert: false}, // Same level, within debounce
			},
			{
				name:          "at higher threshold",
				failures:      7,
				lastAlertTime: time.Now(),
				lastLevel:     50,
				want:          alertResult{level: 66, shouldAlert: true}, // Higher level, ignore debounce
			},
			{
				name:          "at same threshold after higher",
				failures:      8,
				lastAlertTime: time.Now(),
				lastLevel:     66,
				want:          alertResult{level: 66, shouldAlert: false}, // Same level as 7, within debounce
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				level, shouldAlert := evaluator.ShouldAlert(tt.failures, tt.lastAlertTime, tt.lastLevel)
				assert.Equal(t, tt.want, alertResult{level: level, shouldAlert: shouldAlert})
			})
		}
	})

	t.Run("with invalid thresholds", func(t *testing.T) {
		t.Parallel()
		evaluator := alert.NewAlertEvaluator(
			[]int{-5, 0, 101, 150}, // Only invalid thresholds
			100,                    // Makes percentages match failure counts
			0,                      // No debouncing
		)

		tests := []struct {
			name          string
			failures      int64
			lastAlertTime time.Time
			lastLevel     int
			want          alertResult
		}{
			{
				name:      "zero failures",
				failures:  0,
				lastLevel: 0,
				want:      alertResult{level: 0, shouldAlert: false},
			},
			{
				name:      "at auto-disable",
				failures:  100,
				lastLevel: 0,
				want:      alertResult{level: 100, shouldAlert: true},
			},
			{
				name:          "above auto-disable",
				failures:      150,
				lastAlertTime: time.Now(),
				lastLevel:     100,
				want:          alertResult{level: 100, shouldAlert: false}, // Same level, no alert
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				level, shouldAlert := evaluator.ShouldAlert(tt.failures, tt.lastAlertTime, tt.lastLevel)
				assert.Equal(t, tt.want, alertResult{level: level, shouldAlert: shouldAlert})
			})
		}
	})
}

func TestAlertEvaluator_ShouldAlert_Debounce(t *testing.T) {
	t.Parallel()

	evaluator := alert.NewAlertEvaluator(
		[]int{50, 66, 90, 100},
		10,   // autoDisableFailureCount
		1000, // 1 second debouncing
	)

	now := time.Now()
	withinDebounce := now.Add(-500 * time.Millisecond)   // Within 1s window
	outsideDebounce := now.Add(-2000 * time.Millisecond) // Past 1s window

	tests := []struct {
		name          string
		failures      int64
		lastAlertTime time.Time
		lastLevel     int
		wantLevel     int
		wantAlert     bool
	}{
		{
			name:          "same level - within debounce - should not alert",
			failures:      7,  // Level 66
			lastLevel:     66, // Last alert was also level 66
			lastAlertTime: withinDebounce,
			wantLevel:     66,
			wantAlert:     false,
		},
		{
			name:          "same level - outside debounce - should still not alert",
			failures:      7,  // Level 66
			lastLevel:     66, // Last alert was also level 66
			lastAlertTime: outsideDebounce,
			wantLevel:     66,
			wantAlert:     false,
		},
		{
			name:          "higher level - within debounce - should not alert",
			failures:      9,  // Level 90
			lastLevel:     66, // Last alert was level 66
			lastAlertTime: withinDebounce,
			wantLevel:     90,
			wantAlert:     false,
		},
		{
			name:          "higher level - outside debounce - should alert",
			failures:      9,  // Level 90
			lastLevel:     66, // Last alert was level 66
			lastAlertTime: outsideDebounce,
			wantLevel:     90,
			wantAlert:     true,
		},
		{
			name:          "lower level - within debounce - should not alert",
			failures:      5,  // Level 50
			lastLevel:     66, // Last alert was level 66
			lastAlertTime: withinDebounce,
			wantLevel:     50,
			wantAlert:     false,
		},
		{
			name:          "lower level - outside debounce - should not alert (impossible in normal operation)",
			failures:      5,  // Level 50
			lastLevel:     66, // Last alert was level 66
			lastAlertTime: outsideDebounce,
			wantLevel:     50,
			wantAlert:     false, // Lower levels should never alert as they're impossible in normal operation
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotLevel, gotAlert := evaluator.ShouldAlert(tt.failures, tt.lastAlertTime, tt.lastLevel)
			assert.Equal(t, tt.wantLevel, gotLevel)
			assert.Equal(t, tt.wantAlert, gotAlert)
		})
	}
}
