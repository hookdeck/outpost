package alert

import (
	"sort"
)

// AlertEvaluator determines when alerts should be triggered
type AlertEvaluator interface {
	// ShouldAlert determines if an alert should be sent and returns the alert level
	ShouldAlert(failures int) (level int, shouldAlert bool)
}

type thresholdPair struct {
	percentage int
	failures   int
}

type alertEvaluator struct {
	thresholds              []thresholdPair // sorted pairs of percentage and failure counts
	autoDisableFailureCount int
}

// NewAlertEvaluator creates a new alert evaluator
func NewAlertEvaluator(thresholds []int, autoDisableFailureCount int) AlertEvaluator {
	// Create pairs of percentage thresholds and their corresponding failure counts
	finalThresholds := make([]thresholdPair, 0, len(thresholds))

	// Convert percentages to failure counts
	for _, percentage := range thresholds {
		// Skip invalid percentages
		if percentage <= 0 || percentage > 100 {
			continue
		}
		// Ceiling division: (a + b - 1) / b
		failures := (int(autoDisableFailureCount)*int(percentage) + 99) / 100
		finalThresholds = append(finalThresholds, thresholdPair{
			percentage: percentage,
			failures:   failures,
		})
	}

	// Sort by failure count
	sort.Slice(finalThresholds, func(i, j int) bool { return finalThresholds[i].failures < finalThresholds[j].failures })

	// Check if we need to add 100
	needsAutoDisable := true
	if len(finalThresholds) > 0 && finalThresholds[len(finalThresholds)-1].percentage == 100 {
		needsAutoDisable = false
	}

	// Auto-include 100% threshold if not present
	if needsAutoDisable {
		finalThresholds = append(finalThresholds, thresholdPair{
			percentage: 100,
			failures:   autoDisableFailureCount,
		})
	}

	return &alertEvaluator{
		thresholds:              finalThresholds,
		autoDisableFailureCount: autoDisableFailureCount,
	}
}

func (e *alertEvaluator) ShouldAlert(failures int) (int, bool) {
	// If no thresholds configured, never alert
	if len(e.thresholds) == 0 {
		return 0, false
	}

	// Get current alert level
	for i := len(e.thresholds) - 1; i >= 0; i-- {
		if failures == e.thresholds[i].failures {
			return e.thresholds[i].percentage, true
		}
	}

	return 0, false
}
