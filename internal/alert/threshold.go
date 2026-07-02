package alert

import (
	"sort"
)

type thresholdPair struct {
	percentage int
	failures   int
}

// thresholdEvaluator maps a consecutive-failure count to the alert threshold
// it crosses, if any.
type thresholdEvaluator struct {
	thresholds              []thresholdPair // sorted pairs of percentage and failure counts
	autoDisableFailureCount int
}

// newThresholdEvaluator converts percentage thresholds into failure counts
// against the auto-disable count (the 100% denominator), always including the
// 100% threshold.
func newThresholdEvaluator(thresholds []int, autoDisableFailureCount int) thresholdEvaluator {
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

	return thresholdEvaluator{
		thresholds:              finalThresholds,
		autoDisableFailureCount: autoDisableFailureCount,
	}
}

func (e thresholdEvaluator) shouldAlert(failures int) (int, bool) {
	// If no thresholds configured, never alert
	if len(e.thresholds) == 0 {
		return 0, false
	}

	// Get current alert level
	// Iterate from highest to lowest threshold
	for i := len(e.thresholds) - 1; i >= 0; i-- {
		threshold := e.thresholds[i]

		// For the 100% threshold (auto-disable), use >= to ensure we don't miss it
		// if concurrent processing causes us to skip over the exact count.
		// For other thresholds, use exact match to avoid duplicate alerts.
		if threshold.percentage == 100 {
			if failures >= threshold.failures {
				return threshold.percentage, true
			}
		} else {
			if failures == threshold.failures {
				return threshold.percentage, true
			}
		}
	}

	return 0, false
}
