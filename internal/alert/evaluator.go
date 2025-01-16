package alert

import (
	"sort"
	"time"
)

type thresholdPair struct {
	percentage int
	failures   int64
}

type alertEvaluator struct {
	thresholds              []thresholdPair // sorted pairs of percentage and failure counts
	autoDisableFailureCount int
	debouncingIntervalMS    int64
}

// NewAlertEvaluator creates a new alert evaluator
func NewAlertEvaluator(config AlertConfig) AlertEvaluator {
	// Create pairs of percentage thresholds and their corresponding failure counts
	thresholds := make([]thresholdPair, len(config.AlertThresholds))
	for i, percentage := range config.AlertThresholds {
		// Ceiling division: (a + b - 1) / b
		failures := (int64(config.AutoDisableFailureCount)*int64(percentage) + 99) / 100
		thresholds[i] = thresholdPair{
			percentage: percentage,
			failures:   failures,
		}
	}
	// Sort by failure count
	sort.Slice(thresholds, func(i, j int) bool { return thresholds[i].failures < thresholds[j].failures })

	return &alertEvaluator{
		thresholds:              thresholds,
		autoDisableFailureCount: config.AutoDisableFailureCount,
		debouncingIntervalMS:    config.DebouncingIntervalMS,
	}
}

func (e *alertEvaluator) ShouldAlert(failures int64, lastAlertTime time.Time, lastAlertLevel int) (int, bool) {
	// If no thresholds configured, never alert
	if len(e.thresholds) == 0 {
		return 0, false
	}

	// Get current alert level
	level := e.GetAlertLevel(failures)
	if level == 0 {
		return 0, false
	}

	// If no previous alert, we can alert immediately
	if lastAlertTime.IsZero() {
		return level, true
	}

	// If within debounce window, never alert
	if time.Since(lastAlertTime).Milliseconds() < e.debouncingIntervalMS {
		return level, false
	}

	// After debounce window:
	// - If at same level as last alert, don't alert
	// - If at lower level, don't alert (impossible in normal operation)
	// - Only alert for higher levels
	if level <= lastAlertLevel {
		return level, false
	}

	return level, true
}

func (e *alertEvaluator) GetAlertLevel(failures int64) int {
	// If no thresholds configured, return 0
	if len(e.thresholds) == 0 {
		return 0
	}

	// Check each threshold in reverse order to get the highest threshold we've exceeded
	for i := len(e.thresholds) - 1; i >= 0; i-- {
		if failures >= e.thresholds[i].failures {
			return e.thresholds[i].percentage
		}
	}

	return 0
}
