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
	fullRange := ds.dateRange.toDriver()
	denseRange := ds.denseDayRange.toDriver()

	// ── Event Metrics ──────────────────────────────────────────────────

	t.Run("EventMetrics", func(t *testing.T) {
		t.Run("count all", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID:  ds.tenant1,
				DateRange: fullRange,
				Measures:  []string{"count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 300, *resp.Data[0].Count)
		})

		t.Run("by topic", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID:   ds.tenant1,
				DateRange:  fullRange,
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
				TenantID:   ds.tenant1,
				DateRange:  fullRange,
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

		t.Run("by eligible_for_retry", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID:   ds.tenant1,
				DateRange:  fullRange,
				Measures:   []string{"count"},
				Dimensions: []string{"eligible_for_retry"},
			})
			require.NoError(t, err)
			assert.Len(t, resp.Data, 2)

			ec := map[bool]int{}
			for _, dp := range resp.Data {
				require.NotNil(t, dp.EligibleForRetry)
				require.NotNil(t, dp.Count)
				ec[*dp.EligibleForRetry] = *dp.Count
			}
			assert.Equal(t, 200, ec[true])
			assert.Equal(t, 100, ec[false])
		})

		t.Run("filter by topic", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID:  ds.tenant1,
				DateRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"topic": {testutil.TestTopics[0]}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 100, *resp.Data[0].Count)
		})

		t.Run("filter by destination_id", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID:  ds.tenant1,
				DateRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"destination_id": {ds.dest1_1}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 150, *resp.Data[0].Count)
		})

		t.Run("tenant isolation", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID:  ds.tenant2,
				DateRange: fullRange,
				Measures:  []string{"count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 5, *resp.Data[0].Count)
		})

		t.Run("empty date range", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID: ds.tenant1,
				DateRange: driver.DateRange{
					Start: time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC),
					End:   time.Date(1999, 2, 1, 0, 0, 0, 0, time.UTC),
				},
				Measures: []string{"count"},
			})
			require.NoError(t, err)
			assert.Empty(t, resp.Data)
		})

		t.Run("granularity 1M", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID:    ds.tenant1,
				DateRange:   fullRange,
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
				TenantID:    ds.tenant1,
				DateRange:   fullRange,
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

		t.Run("granularity 1d on dense day range", func(t *testing.T) {
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID:    ds.tenant1,
				DateRange:   denseRange,
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
				TenantID:    ds.tenant1,
				DateRange:   denseRange,
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
			hour10Range := driver.DateRange{
				Start: time.Date(2000, 1, 15, 10, 0, 0, 0, time.UTC),
				End:   time.Date(2000, 1, 15, 11, 0, 0, 0, time.UTC),
			}
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID:    ds.tenant1,
				DateRange:   hour10Range,
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
			hour12Range := driver.DateRange{
				Start: time.Date(2000, 1, 15, 12, 0, 0, 0, time.UTC),
				End:   time.Date(2000, 1, 15, 13, 0, 0, 0, time.UTC),
			}
			resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
				TenantID:    ds.tenant1,
				DateRange:   hour12Range,
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
				TenantID:  ds.tenant1,
				DateRange: fullRange,
				Measures:  []string{"count"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 300, *resp.Data[0].Count)
		})

		t.Run("successful and failed counts", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TenantID:  ds.tenant1,
				DateRange: fullRange,
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
				TenantID:  ds.tenant1,
				DateRange: fullRange,
				Measures:  []string{"error_rate"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].ErrorRate)
			assert.InDelta(t, 0.4, *resp.Data[0].ErrorRate, 0.001)
		})

		t.Run("retry measures", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TenantID:  ds.tenant1,
				DateRange: fullRange,
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
			assert.InDelta(t, 1.5, *dp.AvgAttemptNumber, 0.001)
		})

		t.Run("by status", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TenantID:   ds.tenant1,
				DateRange:  fullRange,
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
				TenantID:   ds.tenant1,
				DateRange:  fullRange,
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

		t.Run("by code", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TenantID:   ds.tenant1,
				DateRange:  fullRange,
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
				TenantID:  ds.tenant1,
				DateRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"status": {"failed"}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 120, *resp.Data[0].Count)
		})

		t.Run("filter by topic", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TenantID:  ds.tenant1,
				DateRange: fullRange,
				Measures:  []string{"count"},
				Filters:   map[string][]string{"topic": {testutil.TestTopics[0]}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Data, 1)
			require.NotNil(t, resp.Data[0].Count)
			assert.Equal(t, 100, *resp.Data[0].Count)
		})

		t.Run("granularity 1h on dense day", func(t *testing.T) {
			resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
				TenantID:    ds.tenant1,
				DateRange:   denseRange,
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
				TenantID:  ds.tenant2,
				DateRange: fullRange,
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
			TenantID:  ds.tenant1,
			DateRange: fullRange,
			Measures:  []string{"count"},
		})
		require.NoError(t, err)
		assert.Equal(t, 1, resp.Metadata.RowCount)
		assert.False(t, resp.Metadata.Truncated)
		assert.Greater(t, resp.Metadata.RowLimit, 0)
	})
}
