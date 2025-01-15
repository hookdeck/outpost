package alert

import (
	"sort"
	"time"
)

type alertEvaluator struct{}

// NewAlertEvaluator creates a new alert evaluator
func NewAlertEvaluator() AlertEvaluator {
	return &alertEvaluator{}
}

func (e *alertEvaluator) ShouldAlert(failures int64, lastAlertTime time.Time, config AlertConfig) bool {
	// If no previous alert, we should alert
	if lastAlertTime.IsZero() {
		return true
	}

	// Check debouncing interval
	if time.Since(lastAlertTime).Milliseconds() < config.DebouncingIntervalMS {
		return false
	}

	// Check if we've hit a threshold percentage
	level, hit := e.GetAlertLevel(failures, config)
	return hit && level > 0
}

func (e *alertEvaluator) GetAlertLevel(failures int64, config AlertConfig) (int, bool) {
	// Use default thresholds if none configured
	thresholds := config.AlertThresholds
	if len(thresholds) == 0 {
		thresholds = []int{50, 70, 90, 100}
	}

	// Sort thresholds in ascending order
	sort.Ints(thresholds)

	// Check each threshold
	for _, threshold := range thresholds {
		failuresForThreshold := (int64(config.AutoDisableFailureCount) * int64(threshold)) / 100
		if failures == failuresForThreshold {
			return threshold, true
		}
	}

	return 0, false
}
