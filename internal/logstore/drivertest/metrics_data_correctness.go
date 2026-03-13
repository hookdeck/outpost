package drivertest

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMetricsDataCorrectness(t *testing.T, ctx context.Context, logStore driver.LogStore, ds *metricsDataset) {
	fullRange := ds.timeRange.toDriver()
	denseRange := ds.denseDayRange.toDriver()

	// ── Event Metrics ──────────────────────────────────────────────────

	t.Run("EventMetrics", func(t *testing.T) {
		t.Run("count all", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange: fullRange,
				Measures:  []string{"count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 300, *resp.Data[0].Count)
		})

		t.Run("by topic", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:    map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:  fullRange,
				Measures:   []string{"count"},
				Dimensions: []string{"topic"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 3)

			tc := map[string]int{}
			for _, dp := range resp.Data {
				require.NotNil(t, dp.Topic)
				require.NotNil(t, dp.Count)
				tc[*dp.Topic] = *dp.Count
			}
			assert.Equal(t, 100, tc[testutil.TestTopics[0]]) // user.created
			assert.Equal(t, 100, tc[testutil.TestTopics[1]]) // user.deleted
			assert.Equal(t, 100, tc[testutil.TestTopics[2]]) // user.updated
		})

		t.Run("by destination_id", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:    map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:  fullRange,
				Measures:   []string{"count"},
				Dimensions: []string{"destination_id"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 2)

			dc := map[string]int{}
			for _, dp := range resp.Data {
				require.NotNil(t, dp.DestinationID)
				require.NotNil(t, dp.Count)
				dc[*dp.DestinationID] = *dp.Count
			}
			assert.Equal(t, 150, dc[ds.dest1_1])
			assert.Equal(t, 150, dc[ds.dest1_2])
		})

		t.Run("by tenant_id", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TimeRange:  fullRange,
				Measures:   []string{"count"},
				Dimensions: []string{"tenant_id"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 2)

			tc := map[string]int{}
			for _, dp := range resp.Data {
				require.NotNil(t, dp.TenantID)
				require.NotNil(t, dp.Count)
				tc[*dp.TenantID] = *dp.Count
			}
			assert.Equal(t, 300, tc[ds.tenant1])
			assert.Equal(t, 5, tc[ds.tenant2])
		})

		t.Run("filter by topic", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TimeRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}, "topic": {testutil.TestTopics[0]}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 100, *resp.Data[0].Count)
		})

		t.Run("filter by destination_id", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TimeRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}, "destination_id": {ds.dest1_1}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 150, *resp.Data[0].Count)
		})

		t.Run("tenant isolation", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:   map[string][]string{"tenant_id": {ds.tenant2}},
				TimeRange: fullRange,
				Measures:  []string{"count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 5, *resp.Data[0].Count)
		})

		t.Run("empty time range", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters: map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange: driver.TimeRange{
					Start: time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC),
					End:   time.Date(1999, 2, 1, 0, 0, 0, 0, time.UTC),
				},
				Measures: []string{"count"},
			})
			require.NoError(t, err)
			assert.Empty(t, resp.Data)
		})

		t.Run("rate no granularity", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange: fullRange,
				Measures:  []string{"rate"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Rate)
			// rate = count / total_seconds = 300 / (31 days * 86400)
			assert.InDelta(t, 300.0/2678400.0, *resp.Data[0].Rate, 0.0000001)
		})

		t.Run("rate with 1h granularity on dense day", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:     map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:   denseRange,
				Granularity: &driver.Granularity{Value: 1, Unit: "h"},
				Measures:    []string{"count", "rate"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 24)

			for _, dp := range resp.Data {
				require.NotNil(t, dp.TimeBucket)
				require.NotNil(t, dp.Count)
				require.NotNil(t, dp.Rate)
				// rate = count / 3600 (1h bucket)
				expected := float64(*dp.Count) / 3600.0
				assert.InDelta(t, expected, *dp.Rate, 0.0000001,
					"hour %d: rate should be count/3600", dp.TimeBucket.Hour())
			}
		})

		t.Run("granularity 1M", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:     map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:   fullRange,
				Granularity: &driver.Granularity{Value: 1, Unit: "M"},
				Measures:    []string{"count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].TimeBucket)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 300, *resp.Data[0].Count)
		})

		t.Run("granularity 1w", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:     map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:   fullRange,
				Granularity: &driver.Granularity{Value: 1, Unit: "w"},
				Measures:    []string{"count"},
			})
			require.NoError(t, err)
			require.NotEmpty(t, resp.Data)

			total := 0
			for _, dp := range resp.Data {
				require.NotNil(t, dp.TimeBucket)
				require.NotNil(t, dp.Count)
				total += *dp.Count
			}
			assert.Equal(t, 300, total)
		})

		t.Run("granularity 2d preserves total count", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:     map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:   fullRange,
				Granularity: &driver.Granularity{Value: 2, Unit: "d"},
				Measures:    []string{"count"},
			})
			require.NoError(t, err)
			require.NotEmpty(t, resp.Data)

			total := 0
			for _, dp := range resp.Data {
				require.NotNil(t, dp.TimeBucket)
				require.NotNil(t, dp.Count)
				total += *dp.Count
			}
			// All 300 events must be accounted for — none silently dropped.
			assert.Equal(t, 300, total)
		})

		t.Run("granularity 1d on dense day range", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:     map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:   denseRange,
				Granularity: &driver.Granularity{Value: 1, Unit: "d"},
				Measures:    []string{"count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 250, *resp.Data[0].Count)
		})

		t.Run("granularity 1h on dense day", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:     map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:   denseRange,
				Granularity: &driver.Granularity{Value: 1, Unit: "h"},
				Measures:    []string{"count"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 24)

			hourly := map[int]int{}
			total := 0
			for _, dp := range resp.Data {
				require.NotNil(t, dp.TimeBucket)
				require.NotNil(t, dp.Count)
				hourly[dp.TimeBucket.Hour()] = *dp.Count
				total += *dp.Count
			}
			assert.Equal(t, 25, hourly[10])
			assert.Equal(t, 50, hourly[11])
			assert.Equal(t, 100, hourly[12])
			assert.Equal(t, 50, hourly[13])
			assert.Equal(t, 25, hourly[14])
			assert.Equal(t, 250, total)
		})

		t.Run("granularity 1m on dense day hour 10", func(t *testing.T) {
			hour10Range := driver.TimeRange{
				Start: time.Date(2000, 1, 15, 10, 0, 0, 0, time.UTC),
				End:   time.Date(2000, 1, 15, 11, 0, 0, 0, time.UTC),
			}
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:     map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:   hour10Range,
				Granularity: &driver.Granularity{Value: 1, Unit: "m"},
				Measures:    []string{"count"},
			})
			require.NoError(t, err)
			// 60 minutes in the hour, bucket filling produces all 60
			assert.Len(t, resp.Data, 60)

			total := 0
			for _, dp := range resp.Data {
				require.NotNil(t, dp.TimeBucket)
				require.NotNil(t, dp.Count)
				total += *dp.Count
			}
			assert.Equal(t, 25, total)
		})

		t.Run("granularity 1m on dense day hour 12", func(t *testing.T) {
			hour12Range := driver.TimeRange{
				Start: time.Date(2000, 1, 15, 12, 0, 0, 0, time.UTC),
				End:   time.Date(2000, 1, 15, 13, 0, 0, 0, time.UTC),
			}
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				Filters:     map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:   hour12Range,
				Granularity: &driver.Granularity{Value: 1, Unit: "m"},
				Measures:    []string{"count"},
			})
			require.NoError(t, err)
			// 60 minutes in the hour, bucket filling produces all 60
			assert.Len(t, resp.Data, 60)

			total := 0
			for _, dp := range resp.Data {
				require.NotNil(t, dp.TimeBucket)
				require.NotNil(t, dp.Count)
				total += *dp.Count
			}
			assert.Equal(t, 100, total)
		})
	})

	// ── Attempt Metrics ────────────────────────────────────────────────

	t.Run("AttemptMetrics", func(t *testing.T) {
		t.Run("count all", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange: fullRange,
				Measures:  []string{"count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 300, *resp.Data[0].Count)
		})

		t.Run("successful and failed counts", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange: fullRange,
				Measures:  []string{"count", "successful_count", "failed_count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			dp := resp.Data[0]
			require.NotNil(t, dp.Count)
			require.NotNil(t, dp.SuccessfulCount)
			require.NotNil(t, dp.FailedCount)
			assert.Equal(t, 300, *dp.Count)
			assert.Equal(t, 180, *dp.SuccessfulCount)
			assert.Equal(t, 120, *dp.FailedCount)
		})

		t.Run("error rate", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange: fullRange,
				Measures:  []string{"error_rate"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].ErrorRate)
			assert.InDelta(t, 0.4, *resp.Data[0].ErrorRate, 0.001)
		})

		t.Run("retry measures", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange: fullRange,
				Measures:  []string{"first_attempt_count", "retry_count", "manual_retry_count", "avg_attempt_number"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			dp := resp.Data[0]
			require.NotNil(t, dp.FirstAttemptCount)
			require.NotNil(t, dp.RetryCount)
			require.NotNil(t, dp.ManualRetryCount)
			require.NotNil(t, dp.AvgAttemptNumber)
			assert.Equal(t, 75, *dp.FirstAttemptCount)
			assert.Equal(t, 225, *dp.RetryCount)
			assert.Equal(t, 30, *dp.ManualRetryCount)
			assert.InDelta(t, 2.5, *dp.AvgAttemptNumber, 0.001)
		})

		t.Run("rate no granularity", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange: fullRange,
				Measures:  []string{"rate", "successful_rate", "failed_rate"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			dp := resp.Data[0]
			require.NotNil(t, dp.Rate)
			require.NotNil(t, dp.SuccessfulRate)
			require.NotNil(t, dp.FailedRate)
			// total_seconds = 31 days * 86400 = 2678400
			assert.InDelta(t, 300.0/2678400.0, *dp.Rate, 0.0000001)
			assert.InDelta(t, 180.0/2678400.0, *dp.SuccessfulRate, 0.0000001)
			assert.InDelta(t, 120.0/2678400.0, *dp.FailedRate, 0.0000001)
		})

		t.Run("rate with 1h granularity on dense day", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:     map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:   denseRange,
				Granularity: &driver.Granularity{Value: 1, Unit: "h"},
				Measures:    []string{"count", "rate"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 24)

			for _, dp := range resp.Data {
				require.NotNil(t, dp.TimeBucket)
				require.NotNil(t, dp.Count)
				require.NotNil(t, dp.Rate)
				expected := float64(*dp.Count) / 3600.0
				assert.InDelta(t, expected, *dp.Rate, 0.0000001,
					"hour %d: rate should be count/3600", dp.TimeBucket.Hour())
			}
		})

		t.Run("by status", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:    map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:  fullRange,
				Measures:   []string{"count"},
				Dimensions: []string{"status"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 2)

			sc := map[string]int{}
			for _, dp := range resp.Data {
				require.NotNil(t, dp.Status)
				require.NotNil(t, dp.Count)
				sc[*dp.Status] = *dp.Count
			}
			assert.Equal(t, 180, sc["success"])
			assert.Equal(t, 120, sc["failed"])
		})

		t.Run("by destination_id", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:    map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:  fullRange,
				Measures:   []string{"count"},
				Dimensions: []string{"destination_id"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 2)

			dc := map[string]int{}
			for _, dp := range resp.Data {
				require.NotNil(t, dp.DestinationID)
				require.NotNil(t, dp.Count)
				dc[*dp.DestinationID] = *dp.Count
			}
			assert.Equal(t, 150, dc[ds.dest1_1])
			assert.Equal(t, 150, dc[ds.dest1_2])
		})

		t.Run("by tenant_id", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TimeRange:  fullRange,
				Measures:   []string{"count"},
				Dimensions: []string{"tenant_id"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 2)

			tc := map[string]int{}
			for _, dp := range resp.Data {
				require.NotNil(t, dp.TenantID)
				require.NotNil(t, dp.Count)
				tc[*dp.TenantID] = *dp.Count
			}
			assert.Equal(t, 300, tc[ds.tenant1])
			assert.Equal(t, 5, tc[ds.tenant2])
		})

		t.Run("by attempt_number", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:    map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:  fullRange,
				Measures:   []string{"count"},
				Dimensions: []string{"attempt_number"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 4)

			ac := map[int]int{}
			for _, dp := range resp.Data {
				require.NotNil(t, dp.AttemptNumber)
				require.NotNil(t, dp.Count)
				ac[*dp.AttemptNumber] = *dp.Count
			}
			// attempt_number = i % 4 + 1 → each value appears 75 times
			assert.Equal(t, 75, ac[1])
			assert.Equal(t, 75, ac[2])
			assert.Equal(t, 75, ac[3])
			assert.Equal(t, 75, ac[4])
		})

		t.Run("by code", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:    map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:  fullRange,
				Measures:   []string{"count"},
				Dimensions: []string{"code"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 4)

			cc := map[string]int{}
			for _, dp := range resp.Data {
				require.NotNil(t, dp.Code)
				require.NotNil(t, dp.Count)
				cc[*dp.Code] = *dp.Count
			}
			assert.Equal(t, 90, cc["200"])
			assert.Equal(t, 90, cc["201"])
			assert.Equal(t, 60, cc["500"])
			assert.Equal(t, 60, cc["422"])
		})

		t.Run("filter by status", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TimeRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}, "status": {"failed"}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 120, *resp.Data[0].Count)
		})

		t.Run("filter by topic", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TimeRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}, "topic": {testutil.TestTopics[0]}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 100, *resp.Data[0].Count)
		})

		t.Run("filter by code", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TimeRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}, "code": {"500"}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 60, *resp.Data[0].Count)
		})

		t.Run("filter by manual", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TimeRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}, "manual": {"true"}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 30, *resp.Data[0].Count)
		})

		t.Run("filter by attempt_number", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TimeRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"tenant_id": {ds.tenant1}, "attempt_number": {"1"}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 75, *resp.Data[0].Count)
		})

		t.Run("granularity 1h on dense day", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:     map[string][]string{"tenant_id": {ds.tenant1}},
				TimeRange:   denseRange,
				Granularity: &driver.Granularity{Value: 1, Unit: "h"},
				Measures:    []string{"count"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 24)

			hourly := map[int]int{}
			total := 0
			for _, dp := range resp.Data {
				require.NotNil(t, dp.TimeBucket)
				require.NotNil(t, dp.Count)
				hourly[dp.TimeBucket.Hour()] = *dp.Count
				total += *dp.Count
			}
			assert.Equal(t, 25, hourly[10])
			assert.Equal(t, 50, hourly[11])
			assert.Equal(t, 100, hourly[12])
			assert.Equal(t, 50, hourly[13])
			assert.Equal(t, 25, hourly[14])
			assert.Equal(t, 250, total)
		})

		t.Run("tenant isolation", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				Filters:   map[string][]string{"tenant_id": {ds.tenant2}},
				TimeRange: fullRange,
				Measures:  []string{"count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 5, *resp.Data[0].Count)
		})
	})

	// ── Metadata ───────────────────────────────────────────────────────

	t.Run("Metadata", func(t *testing.T) {
		resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
			Filters:   map[string][]string{"tenant_id": {ds.tenant1}},
			TimeRange: fullRange,
			Measures:  []string{"count"},
		})
		require.NoError(t, err)
		assert.Equal(t, 1, resp.Metadata.RowCount)
		assert.False(t, resp.Metadata.Truncated)
		assert.Greater(t, resp.Metadata.RowLimit, 0)
	})
}
