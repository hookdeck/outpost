package driver

import (
	"context"
	"time"
)

type Metrics interface {
	QueryEventMetrics(ctx context.Context, req MetricsRequest) (*EventMetricsResponse, error)
	QueryAttemptMetrics(ctx context.Context, req MetricsRequest) (*AttemptMetricsResponse, error)
}

type TimeRange struct {
	Start time.Time
	End   time.Time
}

type Granularity struct {
	Value int
	Unit  string // s, m, h, d, w, M
}

type MetricsRequest struct {
	TenantID    string
	TimeRange   TimeRange
	Granularity *Granularity
	Measures    []string
	Dimensions  []string
	Filters     map[string][]string
}

type MetricsMetadata struct {
	Granularity string
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
