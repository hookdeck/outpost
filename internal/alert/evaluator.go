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

func (e *alertEvaluator) ShouldAlert(failures int64, lastAlertTime time.Time) bool {
	// If no thresholds configured, never alert
	if len(e.thresholds) == 0 {
		return false
	}

	// Check if we've hit a threshold percentage
	level, hit := e.GetAlertLevel(failures)
	if !hit || level == 0 {
		return false
	}

	// If no previous alert, we can alert immediately
	if lastAlertTime.IsZero() {
		return true
	}

	// Check debouncing interval
	return time.Since(lastAlertTime).Milliseconds() >= e.debouncingIntervalMS
}

func (e *alertEvaluator) GetAlertLevel(failures int64) (int, bool) {
	// If no thresholds configured, never alert
	if len(e.thresholds) == 0 {
		return 0, false
	}

	// Check each threshold in reverse order to get the highest threshold we've hit
	for i := len(e.thresholds) - 1; i >= 0; i-- {
		if failures == e.thresholds[i].failures {
			return e.thresholds[i].percentage, true
		}
	}

	return 0, false
}
