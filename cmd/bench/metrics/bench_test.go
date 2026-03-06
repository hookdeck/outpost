package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

// ── Date ranges ─────────────────────────────────────────────────────────────

var (
	// Full month — all seeded data lives here.
	fullMonth = driver.DateRange{
		Start: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2000, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	oneDay = driver.DateRange{
		Start: time.Date(2000, 1, 15, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2000, 1, 16, 0, 0, 0, 0, time.UTC),
	}
	oneWeek = driver.DateRange{
		Start: time.Date(2000, 1, 8, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2000, 1, 15, 0, 0, 0, 0, time.UTC),
	}
)

// ── Helpers ─────────────────────────────────────────────────────────────────

func hourly() *driver.Granularity { return &driver.Granularity{Value: 1, Unit: "h"} }
func daily() *driver.Granularity  { return &driver.Granularity{Value: 1, Unit: "d"} }

// ── Event Benchmarks ────────────────────────────────────────────────────────

var eventCases = []struct {
	name string
	req  driver.MetricsRequest
}{
	{
		name: "CountAll",
		req: driver.MetricsRequest{
			TenantID:  "tenant_0",
			DateRange: fullMonth,
			Measures:  []string{"count"},
		},
	},
	{
		name: "CountByTopic",
		req: driver.MetricsRequest{
			TenantID:   "tenant_0",
			DateRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"topic"},
		},
	},
	{
		name: "CountByDestination",
		req: driver.MetricsRequest{
			TenantID:   "tenant_0",
			DateRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"destination_id"},
		},
	},
	{
		name: "Hourly_1Day",
		req: driver.MetricsRequest{
			TenantID:    "tenant_0",
			DateRange:   oneDay,
			Granularity: hourly(),
			Measures:    []string{"count"},
		},
	},
	{
		name: "Hourly_1Week",
		req: driver.MetricsRequest{
			TenantID:    "tenant_0",
			DateRange:   oneWeek,
			Granularity: hourly(),
			Measures:    []string{"count"},
		},
	},
	{
		name: "Daily_1Month",
		req: driver.MetricsRequest{
			TenantID:    "tenant_0",
			DateRange:   fullMonth,
			Granularity: daily(),
			Measures:    []string{"count"},
		},
	},
	{
		name: "FilterByTopic",
		req: driver.MetricsRequest{
			TenantID:  "tenant_0",
			DateRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   map[string][]string{"topic": {"order.created"}},
		},
	},
	{
		name: "SmallTenant",
		req: driver.MetricsRequest{
			TenantID:    "tenant_1",
			DateRange:   fullMonth,
			Granularity: daily(),
			Measures:    []string{"count"},
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
			TenantID:  "tenant_0",
			DateRange: fullMonth,
			Measures:  []string{"count"},
		},
	},
	{
		name: "CountByTopic",
		req: driver.MetricsRequest{
			TenantID:   "tenant_0",
			DateRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"topic"},
		},
	},
	{
		name: "CountByDestination",
		req: driver.MetricsRequest{
			TenantID:   "tenant_0",
			DateRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"destination_id"},
		},
	},
	{
		name: "CountByStatus",
		req: driver.MetricsRequest{
			TenantID:   "tenant_0",
			DateRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"status"},
		},
	},
	{
		name: "Hourly_1Day",
		req: driver.MetricsRequest{
			TenantID:    "tenant_0",
			DateRange:   oneDay,
			Granularity: hourly(),
			Measures:    []string{"count"},
		},
	},
	{
		name: "Hourly_1Week",
		req: driver.MetricsRequest{
			TenantID:    "tenant_0",
			DateRange:   oneWeek,
			Granularity: hourly(),
			Measures:    []string{"count"},
		},
	},
	{
		name: "Daily_1Month",
		req: driver.MetricsRequest{
			TenantID:    "tenant_0",
			DateRange:   fullMonth,
			Granularity: daily(),
			Measures:    []string{"count"},
		},
	},
	{
		name: "AllMeasures",
		req: driver.MetricsRequest{
			TenantID:  "tenant_0",
			DateRange: fullMonth,
			Measures: []string{
				"count",
				"successful_count",
				"failed_count",
				"error_rate",
				"first_attempt_count",
				"retry_count",
				"manual_retry_count",
				"avg_attempt_number",
			},
		},
	},
	{
		name: "FilterByStatus",
		req: driver.MetricsRequest{
			TenantID:  "tenant_0",
			DateRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   map[string][]string{"status": {"failed"}},
		},
	},
	{
		name: "FilterByTopic",
		req: driver.MetricsRequest{
			TenantID:  "tenant_0",
			DateRange: fullMonth,
			Measures:  []string{"count"},
			Filters:   map[string][]string{"topic": {"order.created"}},
		},
	},
	{
		name: "MultiDimension",
		req: driver.MetricsRequest{
			TenantID:   "tenant_0",
			DateRange:  fullMonth,
			Measures:   []string{"count"},
			Dimensions: []string{"topic", "destination_id", "status"},
		},
	},
	{
		name: "SmallTenant",
		req: driver.MetricsRequest{
			TenantID:    "tenant_1",
			DateRange:   fullMonth,
			Granularity: daily(),
			Measures:    []string{"count"},
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
