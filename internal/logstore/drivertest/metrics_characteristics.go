package drivertest

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testMetricsCharacteristics asserts structural properties of the time-series
// response contract (dense bucket filling, ordering, alignment, etc.).
// These tests are independent of specific metric values — they validate the
// shape of the response that dashboard consumers depend on.
//
// Uses the shared dataset from metrics_dataset.go. Key assumptions:
//   - Dense day (Jan 15): data in hours 10-14 only, 250 events
//   - Full range: Jan 2000 (sparse days 3,7,11,22,28 + dense day 15)
//   - 3 topics cycling across all events
func testMetricsCharacteristics(t *testing.T, ctx context.Context, logStore driver.LogStore, ds *metricsDataset) {
	// ── 1. Empty bucket filling ──────────────────────────────────────────
	// Dense day (Jan 15) has data in hours 10-14 only. With 1h granularity
	// over the full day, all 24 hours must be present.

	t.Run("empty bucket filling (events)", func(t *testing.T) {
		resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
			TenantID:    ds.tenant1,
			DateRange:   ds.denseDayRange.toDriver(),
			Granularity: &driver.Granularity{Value: 1, Unit: "h"},
			Measures:    []string{"count"},
		})
		require.NoError(t, err)
		require.Len(t, resp.Data, 24, "24h range with 1h granularity must produce 24 buckets")

		for _, dp := range resp.Data {
			require.NotNil(t, dp.TimeBucket, "every bucket must have a time_bucket")
			require.NotNil(t, dp.Count, "every bucket must have a count")
			h := dp.TimeBucket.Hour()
			if h < 10 || h > 14 {
				assert.Equal(t, 0, *dp.Count, "hour %d should have count=0", h)
			} else {
				assert.Greater(t, *dp.Count, 0, "hour %d should have count>0", h)
			}
		}
	})

	t.Run("empty bucket filling (attempts)", func(t *testing.T) {
		resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
			TenantID:    ds.tenant1,
			DateRange:   ds.denseDayRange.toDriver(),
			Granularity: &driver.Granularity{Value: 1, Unit: "h"},
			Measures:    []string{"count", "error_rate"},
		})
		require.NoError(t, err)
		require.Len(t, resp.Data, 24, "24h range with 1h granularity must produce 24 buckets")

		for _, dp := range resp.Data {
			require.NotNil(t, dp.TimeBucket)
			require.NotNil(t, dp.Count)
			require.NotNil(t, dp.ErrorRate, "error_rate must be present in every bucket")
			h := dp.TimeBucket.Hour()
			if h < 10 || h > 14 {
				assert.Equal(t, 0, *dp.Count, "hour %d should have count=0", h)
				assert.Equal(t, 0.0, *dp.ErrorRate, "hour %d should have error_rate=0.0", h)
			}
		}
	})

	// ── 2. Chronological ordering ────────────────────────────────────────
	// Buckets must be sorted by time_bucket ASC.

	t.Run("chronological ordering (events)", func(t *testing.T) {
		resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
			TenantID:    ds.tenant1,
			DateRange:   ds.dateRange.toDriver(),
			Granularity: &driver.Granularity{Value: 1, Unit: "d"},
			Measures:    []string{"count"},
		})
		require.NoError(t, err)
		require.True(t, len(resp.Data) > 1, "need multiple buckets to test ordering")

		for i := 1; i < len(resp.Data); i++ {
			require.NotNil(t, resp.Data[i-1].TimeBucket)
			require.NotNil(t, resp.Data[i].TimeBucket)
			assert.True(t,
				resp.Data[i-1].TimeBucket.Before(*resp.Data[i].TimeBucket),
				"bucket %d (%s) must be before bucket %d (%s)",
				i-1, resp.Data[i-1].TimeBucket, i, resp.Data[i].TimeBucket,
			)
		}
	})

	t.Run("chronological ordering (attempts)", func(t *testing.T) {
		resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
			TenantID:    ds.tenant1,
			DateRange:   ds.dateRange.toDriver(),
			Granularity: &driver.Granularity{Value: 1, Unit: "d"},
			Measures:    []string{"count"},
		})
		require.NoError(t, err)
		require.True(t, len(resp.Data) > 1)

		for i := 1; i < len(resp.Data); i++ {
			require.NotNil(t, resp.Data[i-1].TimeBucket)
			require.NotNil(t, resp.Data[i].TimeBucket)
			assert.True(t,
				resp.Data[i-1].TimeBucket.Before(*resp.Data[i].TimeBucket),
				"bucket %d (%s) must be before bucket %d (%s)",
				i-1, resp.Data[i-1].TimeBucket, i, resp.Data[i].TimeBucket,
			)
		}
	})

	// ── 3. Deterministic bucket count ────────────────────────────────────
	// The number of buckets depends only on the date range and granularity,
	// never on the density of data.

	t.Run("deterministic bucket count", func(t *testing.T) {
		cases := []struct {
			name     string
			start    time.Time
			end      time.Time
			gran     driver.Granularity
			expected int
		}{
			{
				name:     "24h at 1h",
				start:    time.Date(2000, 1, 15, 0, 0, 0, 0, time.UTC),
				end:      time.Date(2000, 1, 16, 0, 0, 0, 0, time.UTC),
				gran:     driver.Granularity{Value: 1, Unit: "h"},
				expected: 24,
			},
			{
				name:     "7d at 1d",
				start:    time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
				end:      time.Date(2000, 1, 8, 0, 0, 0, 0, time.UTC),
				gran:     driver.Granularity{Value: 1, Unit: "d"},
				expected: 7,
			},
			{
				name:     "1h at 1m",
				start:    time.Date(2000, 1, 15, 10, 0, 0, 0, time.UTC),
				end:      time.Date(2000, 1, 15, 11, 0, 0, 0, time.UTC),
				gran:     driver.Granularity{Value: 1, Unit: "m"},
				expected: 60,
			},
			{
				name:     "1h at 5m",
				start:    time.Date(2000, 1, 15, 10, 0, 0, 0, time.UTC),
				end:      time.Date(2000, 1, 15, 11, 0, 0, 0, time.UTC),
				gran:     driver.Granularity{Value: 5, Unit: "m"},
				expected: 12,
			},
			{
				name:     "granularity larger than range",
				start:    time.Date(2000, 1, 15, 10, 0, 0, 0, time.UTC),
				end:      time.Date(2000, 1, 15, 16, 0, 0, 0, time.UTC),
				gran:     driver.Granularity{Value: 1, Unit: "d"},
				expected: 1,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
					TenantID:    ds.tenant1,
					DateRange:   driver.DateRange{Start: tc.start, End: tc.end},
					Granularity: &tc.gran,
					Measures:    []string{"count"},
				})
				require.NoError(t, err)
				assert.Len(t, resp.Data, tc.expected, "expected %d buckets for %s", tc.expected, tc.name)
			})
		}
	})

	// ── 4. Explicit zero measures ────────────────────────────────────────
	// Empty buckets must have concrete zero values, never nil.

	t.Run("explicit zero measures (events)", func(t *testing.T) {
		resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
			TenantID:    ds.tenant1,
			DateRange:   ds.denseDayRange.toDriver(),
			Granularity: &driver.Granularity{Value: 1, Unit: "h"},
			Measures:    []string{"count"},
		})
		require.NoError(t, err)
		// Guard: need 24 buckets for this test to be meaningful (not vacuously pass).
		require.Len(t, resp.Data, 24, "prerequisite: bucket filling must produce 24 buckets")

		for _, dp := range resp.Data {
			if dp.TimeBucket != nil && (dp.TimeBucket.Hour() < 10 || dp.TimeBucket.Hour() > 14) {
				require.NotNil(t, dp.Count, "count must not be nil in empty bucket at %s", dp.TimeBucket)
				assert.Equal(t, 0, *dp.Count)
			}
		}
	})

	t.Run("explicit zero measures (attempts)", func(t *testing.T) {
		resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
			TenantID:    ds.tenant1,
			DateRange:   ds.denseDayRange.toDriver(),
			Granularity: &driver.Granularity{Value: 1, Unit: "h"},
			Measures:    []string{"count", "successful_count", "failed_count", "error_rate", "first_attempt_count", "retry_count", "manual_retry_count", "avg_attempt_number"},
		})
		require.NoError(t, err)
		// Guard: need 24 buckets for this test to be meaningful (not vacuously pass).
		require.Len(t, resp.Data, 24, "prerequisite: bucket filling must produce 24 buckets")

		for _, dp := range resp.Data {
			if dp.TimeBucket != nil && (dp.TimeBucket.Hour() < 10 || dp.TimeBucket.Hour() > 14) {
				require.NotNil(t, dp.Count, "count must not be nil at %s", dp.TimeBucket)
				require.NotNil(t, dp.SuccessfulCount, "successful_count must not be nil at %s", dp.TimeBucket)
				require.NotNil(t, dp.FailedCount, "failed_count must not be nil at %s", dp.TimeBucket)
				require.NotNil(t, dp.ErrorRate, "error_rate must not be nil at %s", dp.TimeBucket)
				require.NotNil(t, dp.FirstAttemptCount, "first_attempt_count must not be nil at %s", dp.TimeBucket)
				require.NotNil(t, dp.RetryCount, "retry_count must not be nil at %s", dp.TimeBucket)
				require.NotNil(t, dp.ManualRetryCount, "manual_retry_count must not be nil at %s", dp.TimeBucket)
				require.NotNil(t, dp.AvgAttemptNumber, "avg_attempt_number must not be nil at %s", dp.TimeBucket)
				assert.Equal(t, 0, *dp.Count)
				assert.Equal(t, 0, *dp.SuccessfulCount)
				assert.Equal(t, 0, *dp.FailedCount)
				assert.Equal(t, 0.0, *dp.ErrorRate, "error_rate must be 0.0, not NaN")
				assert.Equal(t, 0, *dp.FirstAttemptCount)
				assert.Equal(t, 0, *dp.RetryCount)
				assert.Equal(t, 0, *dp.ManualRetryCount)
				assert.Equal(t, 0.0, *dp.AvgAttemptNumber, "avg_attempt_number must be 0.0, not NaN")
			}
		}
	})

	// ── 5. No-data range returns full bucket series ──────────────────────
	// Querying a range with zero matching events still produces the full
	// bucket series, all with zero values.

	t.Run("no-data range (events)", func(t *testing.T) {
		// Feb 2000 is 29 days (leap year).
		resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
			TenantID: ds.tenant1,
			DateRange: driver.DateRange{
				Start: time.Date(2000, 2, 1, 0, 0, 0, 0, time.UTC),
				End:   time.Date(2000, 3, 1, 0, 0, 0, 0, time.UTC),
			},
			Granularity: &driver.Granularity{Value: 1, Unit: "d"},
			Measures:    []string{"count"},
		})
		require.NoError(t, err)
		assert.Len(t, resp.Data, 29, "Feb 2000 (leap year) with 1d granularity must produce 29 buckets")

		for _, dp := range resp.Data {
			require.NotNil(t, dp.Count)
			assert.Equal(t, 0, *dp.Count, "all buckets in no-data range must be zero")
		}
	})

	t.Run("no-data range (attempts)", func(t *testing.T) {
		resp, err := logStore.QueryAttemptMetrics(ctx, driver.MetricsRequest{
			TenantID: ds.tenant1,
			DateRange: driver.DateRange{
				Start: time.Date(2000, 2, 1, 0, 0, 0, 0, time.UTC),
				End:   time.Date(2000, 3, 1, 0, 0, 0, 0, time.UTC),
			},
			Granularity: &driver.Granularity{Value: 1, Unit: "d"},
			Measures:    []string{"count"},
		})
		require.NoError(t, err)
		assert.Len(t, resp.Data, 29)

		for _, dp := range resp.Data {
			require.NotNil(t, dp.Count)
			assert.Equal(t, 0, *dp.Count)
		}
	})

	// ── 6. Bucket alignment ─────────────────────────────────────────────
	// When start doesn't fall on a granularity boundary, buckets still
	// snap to the boundary (e.g., 1h → :00:00).

	t.Run("bucket alignment (1h)", func(t *testing.T) {
		resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
			TenantID: ds.tenant1,
			DateRange: driver.DateRange{
				Start: time.Date(2000, 1, 15, 3, 17, 42, 0, time.UTC),
				End:   time.Date(2000, 1, 15, 8, 0, 0, 0, time.UTC),
			},
			Granularity: &driver.Granularity{Value: 1, Unit: "h"},
			Measures:    []string{"count"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.Data)

		// First bucket should be 03:00:00, not 03:17:42.
		require.NotNil(t, resp.Data[0].TimeBucket)
		assert.Equal(t, 3, resp.Data[0].TimeBucket.Hour())
		assert.Equal(t, 0, resp.Data[0].TimeBucket.Minute())
		assert.Equal(t, 0, resp.Data[0].TimeBucket.Second())

		// All buckets must be on :00:00 boundaries.
		for i, dp := range resp.Data {
			require.NotNil(t, dp.TimeBucket)
			assert.Equal(t, 0, dp.TimeBucket.Minute(), "bucket %d minute must be 0", i)
			assert.Equal(t, 0, dp.TimeBucket.Second(), "bucket %d second must be 0", i)
		}
	})

	t.Run("bucket alignment (1d)", func(t *testing.T) {
		resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
			TenantID: ds.tenant1,
			DateRange: driver.DateRange{
				Start: time.Date(2000, 1, 3, 14, 30, 0, 0, time.UTC),
				End:   time.Date(2000, 1, 6, 0, 0, 0, 0, time.UTC),
			},
			Granularity: &driver.Granularity{Value: 1, Unit: "d"},
			Measures:    []string{"count"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.Data)

		// First bucket should be Jan 3 00:00:00, not 14:30:00.
		require.NotNil(t, resp.Data[0].TimeBucket)
		assert.Equal(t, 3, resp.Data[0].TimeBucket.Day())
		assert.Equal(t, 0, resp.Data[0].TimeBucket.Hour())

		// All buckets at midnight.
		for i, dp := range resp.Data {
			require.NotNil(t, dp.TimeBucket)
			assert.Equal(t, 0, dp.TimeBucket.Hour(), "bucket %d must be at midnight", i)
			assert.Equal(t, 0, dp.TimeBucket.Minute(), "bucket %d must be at midnight", i)
		}
	})

	// ── 7. Dimensions don't multiply empty buckets ───────────────────────
	// With dimensions, empty time slots are only filled for dimension
	// combinations that actually appear in the data (within the full date
	// range). We must not get a cartesian product of all dimensions × all
	// time slots.

	t.Run("dimensions don't cartesian-explode empty buckets", func(t *testing.T) {
		// Query dense day with 1h granularity and dimension=topic.
		// 3 topics exist in the data, data spans hours 10-14.
		// Expected: 5 hours with data × 3 topics + 19 empty hours × (only topics
		// that appear in data for the queried range, filled per-combo along time axis).
		//
		// The key invariant: we must NOT get rows for topic+hour combos where
		// that specific topic never appears anywhere in the query range.
		resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
			TenantID:    ds.tenant1,
			DateRange:   ds.denseDayRange.toDriver(),
			Granularity: &driver.Granularity{Value: 1, Unit: "h"},
			Measures:    []string{"count"},
			Dimensions:  []string{"topic"},
		})
		require.NoError(t, err)

		// Count unique topics in the response.
		topics := map[string]bool{}
		for _, dp := range resp.Data {
			if dp.Topic != nil {
				topics[*dp.Topic] = true
			}
		}
		numTopics := len(topics)

		// Each topic that appears should have exactly 24 buckets (one per hour).
		assert.Len(t, resp.Data, numTopics*24,
			"each topic must have 24 hourly buckets (dense filling per dimension combo)")

		// Verify zero-filled buckets exist for each topic in empty hours.
		type topicHour struct {
			topic string
			hour  int
		}
		counts := map[topicHour]int{}
		for _, dp := range resp.Data {
			if dp.Topic != nil && dp.TimeBucket != nil && dp.Count != nil {
				counts[topicHour{*dp.Topic, dp.TimeBucket.Hour()}] = *dp.Count
			}
		}
		for topic := range topics {
			for h := range 24 {
				_, ok := counts[topicHour{topic, h}]
				assert.True(t, ok, "topic=%s hour=%d must have a bucket", topic, h)
			}
		}
	})

	// ── 8. No granularity → no bucket filling ────────────────────────────
	// When granularity is omitted, bucket filling does not apply.
	// Empty results remain empty (single aggregate row or none).

	t.Run("no granularity no filling", func(t *testing.T) {
		resp, err := logStore.QueryEventMetrics(ctx, driver.MetricsRequest{
			TenantID: ds.tenant1,
			DateRange: driver.DateRange{
				Start: time.Date(2000, 2, 1, 0, 0, 0, 0, time.UTC),
				End:   time.Date(2000, 3, 1, 0, 0, 0, 0, time.UTC),
			},
			Measures: []string{"count"},
		})
		require.NoError(t, err)
		assert.Empty(t, resp.Data, "without granularity, no-data range should return empty")
	})
}
