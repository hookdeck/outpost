package driver

import "time"

// rateDependencies maps each derived rate measure to the count measure it requires.
var rateDependencies = map[string]string{
	"rate":            "count",
	"successful_rate": "successful_count",
	"failed_rate":     "failed_count",
}

// EnrichMeasuresForRates returns a new measures slice with any missing rate
// dependencies appended. For example, if "rate" is requested but "count" is not,
// "count" is added so the SQL query computes it.
func EnrichMeasuresForRates(measures []string) []string {
	seen := make(map[string]struct{}, len(measures))
	for _, m := range measures {
		seen[m] = struct{}{}
	}

	enriched := make([]string, len(measures))
	copy(enriched, measures)

	for _, m := range measures {
		if dep, ok := rateDependencies[m]; ok {
			if _, exists := seen[dep]; !exists {
				enriched = append(enriched, dep)
				seen[dep] = struct{}{}
			}
		}
	}
	return enriched
}

// ComputeEventRates populates Rate fields on event data points from their
// corresponding count fields and the bucket duration.
func ComputeEventRates(data []EventMetricsDataPoint, req MetricsRequest) {
	if !hasMeasure(req.Measures, "rate") {
		return
	}
	for i := range data {
		dp := &data[i]
		dur := bucketDurationSeconds(dp.TimeBucket, req.Granularity, req.TimeRange)
		v := float64(derefIntPtr(dp.Count)) / dur
		dp.Rate = &v
	}
}

// ComputeAttemptRates populates Rate, SuccessfulRate, and FailedRate fields on
// attempt data points from their corresponding count fields and the bucket duration.
func ComputeAttemptRates(data []AttemptMetricsDataPoint, req MetricsRequest) {
	wantRate := hasMeasure(req.Measures, "rate")
	wantSuccessful := hasMeasure(req.Measures, "successful_rate")
	wantFailed := hasMeasure(req.Measures, "failed_rate")
	if !wantRate && !wantSuccessful && !wantFailed {
		return
	}

	for i := range data {
		dp := &data[i]
		dur := bucketDurationSeconds(dp.TimeBucket, req.Granularity, req.TimeRange)
		if wantRate {
			v := float64(derefIntPtr(dp.Count)) / dur
			dp.Rate = &v
		}
		if wantSuccessful {
			v := float64(derefIntPtr(dp.SuccessfulCount)) / dur
			dp.SuccessfulRate = &v
		}
		if wantFailed {
			v := float64(derefIntPtr(dp.FailedCount)) / dur
			dp.FailedRate = &v
		}
	}
}

// bucketDurationSeconds returns the duration of one time bucket in seconds.
func bucketDurationSeconds(timeBucket *time.Time, gran *Granularity, tr TimeRange) float64 {
	if gran == nil {
		return tr.End.Sub(tr.Start).Seconds()
	}
	switch gran.Unit {
	case "s":
		return float64(gran.Value)
	case "m":
		return float64(gran.Value) * 60
	case "h":
		return float64(gran.Value) * 3600
	case "d":
		return float64(gran.Value) * 86400
	case "w":
		return float64(gran.Value) * 7 * 86400
	case "M":
		if timeBucket != nil {
			t := timeBucket.UTC()
			start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
			end := start.AddDate(0, gran.Value, 0)
			return end.Sub(start).Seconds()
		}
		return float64(gran.Value) * 30 * 86400
	default:
		return tr.End.Sub(tr.Start).Seconds()
	}
}

func hasMeasure(measures []string, m string) bool {
	for _, v := range measures {
		if v == m {
			return true
		}
	}
	return false
}

func derefIntPtr(p *int) int {
	if p != nil {
		return *p
	}
	return 0
}
