package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

// ── Time ranges ─────────────────────────────────────────────────────────────

var (
	// Full month — all seeded data lives here.
	fullMonth = driver.TimeRange{
		Start: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2000, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	oneDay = driver.TimeRange{
		Start: time.Date(2000, 1, 15, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2000, 1, 16, 0, 0, 0, 0, time.UTC),
	}
	oneWeek = driver.TimeRange{
		Start: time.Date(2000, 1, 8, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2000, 1, 15, 0, 0, 0, 0, time.UTC),
	}
)

// ── Helpers ─────────────────────────────────────────────────────────────────

func hourly() *driver.Granularity  { return &driver.Granularity{Value: 1, Unit: "h"} }
func daily() *driver.Granularity   { return &driver.Granularity{Value: 1, Unit: "d"} }
func twoDays() *driver.Granularity { return &driver.Granularity{Value: 2, Unit: "d"} }
func weekly() *driver.Granularity  { return &driver.Granularity{Value: 1, Unit: "w"} }
func monthly() *driver.Granularity { return &driver.Granularity{Value: 1, Unit: "M"} }

func tenant0() map[string][]string { return map[string][]string{"tenant_id": {"tenant_0"}} }

func withTenant0(extra map[string][]string) map[string][]string {
	m := tenant0()
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// ── Event Benchmarks ────────────────────────────────────────────────────────

var eventCases = []struct {
	name string
	req  driver.MetricsRequest
}{
	{
		name: "CountAll",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   tenant0(),
		},
	},
	{
		name: "RateAll",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"rate"},
			Filters:   tenant0(),
		},
	},
	{
		name: "CountAndRate",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count", "rate"},
			Filters:   tenant0(),
		},
	},
	{
		name: "CountByTopic",
		req: driver.MetricsRequest{
			TimeRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"topic"},
			Filters:    tenant0(),
		},
	},
	{
		name: "CountByDestination",
		req: driver.MetricsRequest{
			TimeRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"destination_id"},
			Filters:    tenant0(),
		},
	},
	{
		name: "CountByTenant",
		req: driver.MetricsRequest{
			TimeRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"tenant_id"},
		},
	},
	{
		name: "Hourly_1Day",
		req: driver.MetricsRequest{
			TimeRange:   oneDay,
			Granularity: hourly(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "Hourly_1Week",
		req: driver.MetricsRequest{
			TimeRange:   oneWeek,
			Granularity: hourly(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "Daily_1Month",
		req: driver.MetricsRequest{
			TimeRange:   fullMonth,
			Granularity: daily(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "TwoDays_1Month",
		req: driver.MetricsRequest{
			TimeRange:   fullMonth,
			Granularity: twoDays(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "Weekly_1Month",
		req: driver.MetricsRequest{
			TimeRange:   fullMonth,
			Granularity: weekly(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "Monthly_1Month",
		req: driver.MetricsRequest{
			TimeRange:   fullMonth,
			Granularity: monthly(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "RateHourly_1Day",
		req: driver.MetricsRequest{
			TimeRange:   oneDay,
			Granularity: hourly(),
			Measures:    []string{"rate"},
			Filters:     tenant0(),
		},
	},
	{
		name: "FilterByTopic",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   withTenant0(map[string][]string{"topic": {"order.created"}}),
		},
	},
	{
		name: "FilterByDestination",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   withTenant0(map[string][]string{"destination_id": {"dest_0"}}),
		},
	},
	{
		name: "SmallTenant",
		req: driver.MetricsRequest{
			TimeRange:   fullMonth,
			Granularity: daily(),
			Measures:    []string{"count"},
			Filters:     map[string][]string{"tenant_id": {"tenant_1"}},
		},
	},
}

// ── Attempt Benchmarks ──────────────────────────────────────────────────────

var attemptCases = []struct {
	name string
	req  driver.MetricsRequest
}{
	{
		name: "CountAll",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   tenant0(),
		},
	},
	{
		name: "RateAll",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"rate"},
			Filters:   tenant0(),
		},
	},
	{
		name: "SuccessfulRate",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"successful_rate"},
			Filters:   tenant0(),
		},
	},
	{
		name: "FailedRate",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"failed_rate"},
			Filters:   tenant0(),
		},
	},
	{
		name: "CountByTopic",
		req: driver.MetricsRequest{
			TimeRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"topic"},
			Filters:    tenant0(),
		},
	},
	{
		name: "CountByDestination",
		req: driver.MetricsRequest{
			TimeRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"destination_id"},
			Filters:    tenant0(),
		},
	},
	{
		name: "CountByStatus",
		req: driver.MetricsRequest{
			TimeRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"status"},
			Filters:    tenant0(),
		},
	},
	{
		name: "CountByCode",
		req: driver.MetricsRequest{
			TimeRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"code"},
			Filters:    tenant0(),
		},
	},
	{
		name: "CountByAttemptNumber",
		req: driver.MetricsRequest{
			TimeRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"attempt_number"},
			Filters:    tenant0(),
		},
	},
	{
		name: "Hourly_1Day",
		req: driver.MetricsRequest{
			TimeRange:   oneDay,
			Granularity: hourly(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "Hourly_1Week",
		req: driver.MetricsRequest{
			TimeRange:   oneWeek,
			Granularity: hourly(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "Daily_1Month",
		req: driver.MetricsRequest{
			TimeRange:   fullMonth,
			Granularity: daily(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "TwoDays_1Month",
		req: driver.MetricsRequest{
			TimeRange:   fullMonth,
			Granularity: twoDays(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "Weekly_1Month",
		req: driver.MetricsRequest{
			TimeRange:   fullMonth,
			Granularity: weekly(),
			Measures:    []string{"count"},
			Filters:     tenant0(),
		},
	},
	{
		name: "AllMeasures",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures: []string{
				"count",
				"successful_count",
				"failed_count",
				"error_rate",
				"first_attempt_count",
				"retry_count",
				"manual_retry_count",
				"avg_attempt_number",
				"rate",
				"successful_rate",
				"failed_rate",
			},
			Filters: tenant0(),
		},
	},
	{
		name: "AllMeasures_Daily",
		req: driver.MetricsRequest{
			TimeRange:   fullMonth,
			Granularity: daily(),
			Measures: []string{
				"count",
				"successful_count",
				"failed_count",
				"error_rate",
				"rate",
				"successful_rate",
				"failed_rate",
			},
			Filters: tenant0(),
		},
	},
	{
		name: "FilterByStatus",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   withTenant0(map[string][]string{"status": {"failed"}}),
		},
	},
	{
		name: "FilterByCode",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   withTenant0(map[string][]string{"code": {"500"}}),
		},
	},
	{
		name: "FilterByManual",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   withTenant0(map[string][]string{"manual": {"true"}}),
		},
	},
	{
		name: "FilterByAttemptNumber",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   withTenant0(map[string][]string{"attempt_number": {"0"}}),
		},
	},
	{
		name: "FilterByTopic",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   withTenant0(map[string][]string{"topic": {"order.created"}}),
		},
	},
	{
		name: "MultiDimension",
		req: driver.MetricsRequest{
			TimeRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"topic", "destination_id", "status"},
			Filters:    tenant0(),
		},
	},
	{
		name: "MultiFilter",
		req: driver.MetricsRequest{
			TimeRange: fullMonth,
			Measures:  []string{"count"},
			Filters: withTenant0(map[string][]string{
				"status": {"failed"},
				"topic":  {"order.created"},
			}),
		},
	},
	{
		name: "SmallTenant",
		req: driver.MetricsRequest{
			TimeRange:   fullMonth,
			Granularity: daily(),
			Measures:    []string{"count"},
			Filters:     map[string][]string{"tenant_id": {"tenant_1"}},
		},
	},
}

func benchmarkEventMetrics(b *testing.B, store driver.Metrics) {
	ctx := context.Background()

	for _, tc := range eventCases {
		b.Run(tc.name, func(b *testing.B) {
			// Warm up.
			if _, err := store.QueryEventMetrics(ctx, tc.req); err != nil {
				b.Fatalf("warmup: %v", err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := store.QueryEventMetrics(ctx, tc.req); err != nil {
					b.Fatalf("query: %v", err)
				}
			}
		})
	}
}

func benchmarkAttemptMetrics(b *testing.B, store driver.Metrics) {
	ctx := context.Background()

	for _, tc := range attemptCases {
		b.Run(tc.name, func(b *testing.B) {
			// Warm up.
			if _, err := store.QueryAttemptMetrics(ctx, tc.req); err != nil {
				b.Fatalf("warmup: %v", err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := store.QueryAttemptMetrics(ctx, tc.req); err != nil {
					b.Fatalf("query: %v", err)
				}
			}
		})
	}
}
