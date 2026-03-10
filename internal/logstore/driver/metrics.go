package driver

import (
	"context"
	"errors"
	"time"
)

// ErrResourceLimit is returned when a metrics query exceeds server-side
// resource limits (e.g. too many GROUP BY rows, query timeout). Callers
// should surface this as a 400 rather than a 500.
var ErrResourceLimit = errors.New("metrics query exceeded resource limits")

// ErrInvalidTimeRange is returned when the time range is invalid
// (e.g. start >= end). Callers should surface this as a 400.
var ErrInvalidTimeRange = errors.New("invalid time range: start must be before end")

// ValidateMetricsRequest checks that the metrics request is well-formed.
func ValidateMetricsRequest(req MetricsRequest) error {
	if !req.TimeRange.Start.Before(req.TimeRange.End) {
		return ErrInvalidTimeRange
	}
	return nil
}

type Metrics interface {
	QueryEventMetrics(ctx context.Context, req MetricsRequest) (*EventMetricsResponse, error)
	QueryAttemptMetrics(ctx context.Context, req MetricsRequest) (*AttemptMetricsResponse, error)
}

type TimeRange struct {
	Start time.Time
	End   time.Time
}

// Granularity defines the time-bucketing interval for metrics queries.
// For sub-day units (s, m, h), Value controls both step size and alignment
// (e.g. 5m → buckets at :00, :05, :10, …).
// For calendar units (d, w, M), Value controls the step size but alignment
// is always to the start of the natural period (start of day, Sunday-based
// week, or first of month). This means the first bucket may be shorter than
// the requested interval when the query start doesn't fall on a boundary.
type Granularity struct {
	Value int
	Unit  string // s, m, h, d, w, M
}

type MetricsRequest struct {
	TimeRange   TimeRange
	Granularity *Granularity
	Measures    []string
	Dimensions  []string
	Filters     map[string][]string
}

type MetricsMetadata struct {
	QueryTimeMs int64
	RowCount    int
	RowLimit    int
	Truncated   bool
}

// Event metrics

type EventMetricsDataPoint struct {
	TimeBucket *time.Time
	// Measures
	Count *int
	Rate  *float64
	// Dimensions
	TenantID      *string
	Topic         *string
	DestinationID *string
}

type EventMetricsResponse struct {
	Data     []EventMetricsDataPoint
	Metadata MetricsMetadata
}

// Attempt metrics

type AttemptMetricsDataPoint struct {
	TimeBucket *time.Time
	// Measures
	Count             *int
	SuccessfulCount   *int
	FailedCount       *int
	ErrorRate         *float64
	FirstAttemptCount *int
	RetryCount        *int
	ManualRetryCount  *int
	AvgAttemptNumber  *float64
	Rate              *float64
	SuccessfulRate    *float64
	FailedRate        *float64
	// Dimensions
	TenantID      *string
	DestinationID *string
	Topic         *string
	Status        *string
	Code          *string
	Manual        *bool
	AttemptNumber *int
}

type AttemptMetricsResponse struct {
	Data     []AttemptMetricsDataPoint
	Metadata MetricsMetadata
}
